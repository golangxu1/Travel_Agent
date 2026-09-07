// Package amap contains the server-side adapter for the AMap REST APIs.
// It never returns provider URLs containing the API key to callers.
package amap

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"travel-agent/backend-go/internal/domain"
)

const (
	defaultBaseURL     = "https://restapi.amap.com"
	maxPhotoBytes      = 5 << 20
	maxJSONBytes       = 1 << 20
	proxyTokenLifetime = time.Hour
	maxEnrichedPOIs    = 20
)

var (
	ErrUnavailable  = errors.New("amap is not configured")
	ErrInvalidToken = errors.New("invalid media token")
	ErrUpstream     = errors.New("amap upstream request failed")
)

// Client owns AMap credentials and a short-lived, in-memory reference store
// for browser media URLs. The opaque references prevent both the API key and
// original photo URLs from appearing in HTML, JSON, or browser requests.
type Client struct {
	apiKey        string
	baseURL       string
	publicBaseURL string
	httpClient    *http.Client
	signingKey    []byte

	mu     sync.Mutex
	tokens map[string]proxyRecord
}

type proxyRecord struct {
	kind      string
	photoURL  string
	staticMap staticMapRequest
	expiresAt time.Time
}

type staticMapRequest struct {
	Location string
	Zoom     int
	Width    int
	Height   int
	Scale    int
	Markers  []string
}

type geocodeResponse struct {
	Status   string `json:"status"`
	Geocodes []struct {
		Location string `json:"location"`
	} `json:"geocodes"`
}

type placeResponse struct {
	Status string `json:"status"`
	POIs   []struct {
		Name     string `json:"name"`
		Location string `json:"location"`
		Address  string `json:"address"`
		Photos   []struct {
			URL string `json:"url"`
		} `json:"photos"`
	} `json:"pois"`
}

// NewClient creates the production adapter. Passing an empty key leaves the
// optional map features disabled while all planning routes remain usable.
func NewClient(apiKey, publicBaseURL string) *Client {
	return newClient(apiKey, publicBaseURL, defaultBaseURL, newSafeHTTPClient())
}

// NewClientForTest is intentionally exported for package-level tests. It
// accepts a custom HTTP client and base URL, but production code must use
// NewClient so externally fetched photo content uses the safe transport.
func NewClientForTest(apiKey, publicBaseURL, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = newSafeHTTPClient()
	}
	return newClient(apiKey, publicBaseURL, baseURL, httpClient)
}

func newClient(apiKey, publicBaseURL, baseURL string, httpClient *http.Client) *Client {
	if strings.TrimSpace(publicBaseURL) == "" {
		publicBaseURL = "http://localhost:8001"
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	digest := sha256.Sum256([]byte(apiKey))
	return &Client{
		apiKey:        strings.TrimSpace(apiKey),
		baseURL:       strings.TrimRight(baseURL, "/"),
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		httpClient:    httpClient,
		signingKey:    digest[:],
		tokens:        make(map[string]proxyRecord),
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.apiKey != ""
}

// SearchImages preserves the legacy response shape. A missing API key is a
// valid disabled state; actual upstream failures are returned to the handler.
func (c *Client) SearchImages(ctx context.Context, query string, count, poolLimit int) (domain.ImageSearchResult, error) {
	result := domain.ImageSearchResult{Images: []domain.ImageEntry{}, ScenicPool: []domain.ImageEntry{}, Location: nil}
	if !c.Configured() {
		return result, nil
	}
	location, err := c.geocode(ctx, query)
	if err != nil {
		return result, err
	}
	if location == "" {
		return result, nil
	}
	result.Location = &location
	mapURL, err := c.staticMapURL(staticMapRequest{Location: location, Zoom: 11, Width: 750, Height: 400, Scale: 2})
	if err != nil {
		return result, err
	}
	thumbURL, err := c.staticMapURL(staticMapRequest{Location: location, Zoom: 11, Width: 400, Height: 200, Scale: 1})
	if err != nil {
		return result, err
	}
	result.Images = append(result.Images, domain.ImageEntry{
		URL: mapURL, Thumb: thumbURL, Alt: query + " - 城市全景", Credit: "高德地图",
		Link: "https://www.amap.com/search?query=" + url.QueryEscape(query),
	})

	seen := make(map[string]struct{})
	configs := []struct{ keywords, types string }{
		{"风景名胜", "110000"}, {"景点", "110000"}, {"旅游景区", ""},
	}
	for _, config := range configs {
		if len(result.ScenicPool) >= poolLimit {
			break
		}
		places, err := c.searchPlaces(ctx, config.keywords, query, true, config.types, 25)
		if err != nil {
			return domain.ImageSearchResult{}, err
		}
		for _, place := range places {
			if len(result.ScenicPool) >= poolLimit {
				break
			}
			if _, ok := seen[place.Name]; ok || place.Name == "" || len(place.Photos) == 0 {
				continue
			}
			photoURL := place.Photos[0].URL
			proxyURL, err := c.photoURL(photoURL)
			if err != nil {
				continue
			}
			seen[place.Name] = struct{}{}
			result.ScenicPool = append(result.ScenicPool, domain.ImageEntry{
				URL: proxyURL, Thumb: proxyURL, Alt: place.Name, Credit: place.Name,
				Link: "https://www.amap.com/search?query=" + url.QueryEscape(place.Name),
			})
		}
	}
	stripCount := count - 1
	if stripCount < 0 {
		stripCount = 0
	}
	if stripCount > len(result.ScenicPool) {
		stripCount = len(result.ScenicPool)
	}
	result.Images = append(result.Images, result.ScenicPool[:stripCount]...)
	return result, nil
}

// EnrichPOIs fills the legacy POI fields one by one. The bounded loop is an
// intentional concurrency limit: a malformed or unusually long itinerary
// cannot fan out an unbounded number of paid provider calls.
func (c *Client) EnrichPOIs(ctx context.Context, city string, pois []domain.POI) (domain.ItineraryPOIs, error) {
	payload := domain.ItineraryPOIs{POIs: append([]domain.POI(nil), pois...), Maps: map[string]string{}}
	if !c.Configured() {
		return payload, nil
	}
	for index := range payload.POIs {
		if index >= maxEnrichedPOIs {
			break
		}
		poi := &payload.POIs[index]
		places, err := c.findPOI(ctx, city, poi.Name)
		if err == nil {
			for _, place := range places {
				if poi.Location == "" {
					poi.Location = place.Location
				}
				if poi.Address == "" {
					poi.Address = place.Address
				}
				if poi.Photo == "" && len(place.Photos) > 0 {
					if photo, photoErr := c.photoURL(place.Photos[0].URL); photoErr == nil {
						poi.Photo = photo
					}
				}
				if poi.Location != "" {
					break
				}
			}
		}
		if poi.Location == "" {
			if location, geoErr := c.geocode(ctx, city+poi.Name); geoErr == nil {
				poi.Location = location
			}
		}
		if poi.Location != "" {
			if mapURL, mapErr := c.staticMapURL(staticMapRequest{
				Location: poi.Location, Zoom: 16, Width: 640, Height: 280, Scale: 2,
				Markers: []string{"large,0x2563EB,1:" + poi.Location},
			}); mapErr == nil {
				poi.MapThumb = mapURL
			}
		}
	}
	payload.Maps = c.dayMaps(payload.POIs)
	return payload, nil
}

func (c *Client) findPOI(ctx context.Context, city, name string) ([]place, error) {
	attempts := []struct {
		keywords  string
		cityLimit bool
	}{
		{name, true}, {name, false}, {city + name, false},
	}
	for _, attempt := range attempts {
		places, err := c.searchPlaces(ctx, attempt.keywords, city, attempt.cityLimit, "", 5)
		if err != nil {
			return nil, err
		}
		if len(places) > 0 {
			return places, nil
		}
	}
	return nil, nil
}

type place struct {
	Name     string
	Location string
	Address  string
	Photos   []struct{ URL string }
}

func (c *Client) searchPlaces(ctx context.Context, keywords, city string, cityLimit bool, types string, offset int) ([]place, error) {
	values := url.Values{
		"keywords":   {keywords},
		"city":       {city},
		"citylimit":  {strconv.FormatBool(cityLimit)},
		"extensions": {"all"},
		"offset":     {strconv.Itoa(offset)},
	}
	var response placeResponse
	if err := c.getJSON(ctx, "/v3/place/text", values, &response); err != nil {
		return nil, err
	}
	if response.Status != "1" {
		return nil, nil
	}
	places := make([]place, 0, len(response.POIs))
	for _, item := range response.POIs {
		converted := place{Name: item.Name, Location: item.Location, Address: item.Address}
		for _, photo := range item.Photos {
			converted.Photos = append(converted.Photos, struct{ URL string }{URL: photo.URL})
		}
		places = append(places, converted)
	}
	return places, nil
}

func (c *Client) geocode(ctx context.Context, address string) (string, error) {
	var response geocodeResponse
	if err := c.getJSON(ctx, "/v3/geocode/geo", url.Values{"address": {address}, "output": {"json"}}, &response); err != nil {
		return "", err
	}
	if response.Status != "1" || len(response.Geocodes) == 0 {
		return "", nil
	}
	return validLocation(response.Geocodes[0].Location), nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, values url.Values, destination any) error {
	if !c.Configured() {
		return ErrUnavailable
	}
	values.Set("key", c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint+"?"+values.Encode(), nil)
	if err != nil {
		return ErrUpstream
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ErrUpstream
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes+1))
	if err != nil || len(body) > maxJSONBytes || json.Unmarshal(body, destination) != nil {
		return ErrUpstream
	}
	return nil
}

func (c *Client) dayMaps(pois []domain.POI) map[string]string {
	byDay := make(map[int][]domain.POI)
	for _, poi := range pois {
		if validLocation(poi.Location) != "" {
			byDay[poi.Day] = append(byDay[poi.Day], poi)
		}
	}
	days := make([]int, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Ints(days)
	maps := make(map[string]string, len(days))
	for _, day := range days {
		dayPOIs := byDay[day]
		var longitudes, latitudes []float64
		markers := make([]string, 0, len(dayPOIs))
		for index, poi := range dayPOIs {
			longitude, latitude, ok := coordinates(poi.Location)
			if !ok {
				continue
			}
			longitudes = append(longitudes, longitude)
			latitudes = append(latitudes, latitude)
			markers = append(markers, fmt.Sprintf("large,0x2563EB,%d:%s", index+1, poi.Location))
		}
		if len(longitudes) == 0 {
			continue
		}
		location := fmt.Sprintf("%.6f,%.6f", mean(longitudes), mean(latitudes))
		mapURL, err := c.staticMapURL(staticMapRequest{
			Location: location, Zoom: mapZoom(longitudes, latitudes), Width: 750, Height: 400, Scale: 2, Markers: markers,
		})
		if err == nil {
			maps[strconv.Itoa(day)] = mapURL
		}
	}
	return maps
}

func mapZoom(longitudes, latitudes []float64) int {
	if len(longitudes) == 1 {
		return 15
	}
	spread := math.Max(max(longitudes)-min(longitudes), max(latitudes)-min(latitudes))
	switch {
	case spread < 0.015:
		return 14
	case spread < 0.05:
		return 13
	case spread < 0.12:
		return 12
	default:
		return 11
	}
}

func mean(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func min(values []float64) float64 {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func max(values []float64) float64 {
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func validLocation(value string) string {
	if _, _, ok := coordinates(value); !ok {
		return ""
	}
	return value
}

func coordinates(value string) (float64, float64, bool) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	longitude, longitudeErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	latitude, latitudeErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if longitudeErr != nil || latitudeErr != nil || longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90 {
		return 0, 0, false
	}
	return longitude, latitude, true
}

func (c *Client) photoURL(source string) (string, error) {
	if err := validatePhotoSource(source); err != nil {
		return "", err
	}
	token, err := c.issueToken(proxyRecord{kind: "photo", photoURL: source, expiresAt: time.Now().Add(proxyTokenLifetime)})
	if err != nil {
		return "", err
	}
	return c.publicURL("/api/poi-photo", token), nil
}

func (c *Client) staticMapURL(spec staticMapRequest) (string, error) {
	if !c.Configured() {
		return "", ErrUnavailable
	}
	if validLocation(spec.Location) == "" || spec.Zoom < 3 || spec.Zoom > 20 || spec.Width < 1 || spec.Width > 1024 || spec.Height < 1 || spec.Height > 1024 || (spec.Scale != 1 && spec.Scale != 2) {
		return "", errors.New("invalid static map request")
	}
	token, err := c.issueToken(proxyRecord{kind: "static-map", staticMap: spec, expiresAt: time.Now().Add(proxyTokenLifetime)})
	if err != nil {
		return "", err
	}
	return c.publicURL("/api/maps/static", token), nil
}

func (c *Client) publicURL(path, token string) string {
	return c.publicBaseURL + path + "?token=" + url.QueryEscape(token)
}

// ProxyPhoto and ProxyStaticMap are called only by HTTP handlers. Neither
// method accepts a source URL from the browser.
func (c *Client) ProxyPhoto(ctx context.Context, token string) (domain.MediaResponse, error) {
	record, err := c.resolveToken(token, "photo")
	if err != nil {
		return domain.MediaResponse{}, err
	}
	if err := validatePhotoSource(record.photoURL); err != nil {
		return domain.MediaResponse{}, ErrInvalidToken
	}
	return c.fetchImage(ctx, record.photoURL)
}

func (c *Client) ProxyStaticMap(ctx context.Context, token string) (domain.MediaResponse, error) {
	record, err := c.resolveToken(token, "static-map")
	if err != nil {
		return domain.MediaResponse{}, err
	}
	values := url.Values{
		"location": {record.staticMap.Location}, "zoom": {strconv.Itoa(record.staticMap.Zoom)},
		"size": {fmt.Sprintf("%d*%d", record.staticMap.Width, record.staticMap.Height)}, "scale": {strconv.Itoa(record.staticMap.Scale)}, "key": {c.apiKey},
	}
	for _, marker := range record.staticMap.Markers {
		values.Add("markers", marker)
	}
	return c.fetchImage(ctx, c.baseURL+"/v3/staticmap?"+values.Encode())
}

func (c *Client) fetchImage(ctx context.Context, source string) (domain.MediaResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return domain.MediaResponse{}, ErrUpstream
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return domain.MediaResponse{}, ErrUpstream
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return domain.MediaResponse{}, ErrUpstream
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(contentType, "image/") {
		return domain.MediaResponse{}, ErrUpstream
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPhotoBytes+1))
	if err != nil || len(body) > maxPhotoBytes {
		return domain.MediaResponse{}, ErrUpstream
	}
	return domain.MediaResponse{ContentType: contentType, Body: body}, nil
}

func (c *Client) issueToken(record proxyRecord) (string, error) {
	identifierBytes := make([]byte, 24)
	if _, err := rand.Read(identifierBytes); err != nil {
		return "", err
	}
	identifier := base64.RawURLEncoding.EncodeToString(identifierBytes)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, value := range c.tokens {
		if now.After(value.expiresAt) {
			delete(c.tokens, key)
		}
	}
	c.tokens[identifier] = record
	return identifier + "." + c.sign(identifier), nil
}

func (c *Client) resolveToken(token, wantKind string) (proxyRecord, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(c.sign(parts[0]))) {
		return proxyRecord{}, ErrInvalidToken
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record, ok := c.tokens[parts[0]]
	if !ok || record.kind != wantKind || time.Now().After(record.expiresAt) {
		delete(c.tokens, parts[0])
		return proxyRecord{}, ErrInvalidToken
	}
	return record, nil
}

func (c *Client) sign(identifier string) string {
	mac := hmac.New(sha256.New, c.signingKey)
	_, _ = mac.Write([]byte(identifier))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validatePhotoSource(source string) error {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" && parsed.Port() != "443" || !allowedPhotoHost(parsed.Hostname()) {
		return errors.New("untrusted photo source")
	}
	return nil
}

func allowedPhotoHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "amap.com" || host == "amap.com.cn" || strings.HasSuffix(host, ".amap.com") || strings.HasSuffix(host, ".amap.com.cn")
}

func newSafeHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = safeDialContext(dialer)
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 || validatePhotoSource(request.URL.String()) != nil {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func safeDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("host resolution failed")
		}
		for _, ip := range ips {
			if !isPublicIP(net.IP(ip.AsSlice())) {
				return nil, errors.New("non-public address blocked")
			}
		}
		for _, ip := range ips {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return connection, nil
			}
		}
		return nil, errors.New("all resolved addresses failed")
	}
}

func isPublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}
