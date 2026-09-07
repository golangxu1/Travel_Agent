package amap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"travel-agent/backend-go/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func response(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestSearchImagesUsesOpaqueProxyURLs(t *testing.T) {
	client := NewClientForTest("secret-amap-key", "http://go.test", "https://amap.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/v3/geocode/geo":
				return response(http.StatusOK, "application/json", `{"status":"1","geocodes":[{"location":"120.1551,30.2741"}]}`), nil
			case "/v3/place/text":
				return response(http.StatusOK, "application/json", `{"status":"1","pois":[{"name":"西湖","location":"120.1400,30.2400","photos":[{"url":"https://aos-cdn-image.amap.com/west-lake.jpg"}]}]}`), nil
			default:
				return nil, errors.New("unexpected endpoint")
			}
		}),
	})

	result, err := client.SearchImages(context.Background(), "杭州", 4, 24)
	if err != nil {
		t.Fatalf("SearchImages: %v", err)
	}
	if len(result.Images) != 2 || len(result.ScenicPool) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	for _, image := range append(result.Images, result.ScenicPool...) {
		if !strings.HasPrefix(image.URL, "http://go.test/api/") {
			t.Fatalf("not a Go proxy URL: %q", image.URL)
		}
		for _, leaked := range []string{"secret-amap-key", "restapi.amap.com", "west-lake.jpg"} {
			if strings.Contains(image.URL, leaked) {
				t.Fatalf("proxy URL leaked %q: %s", leaked, image.URL)
			}
		}
	}
}

func TestProxyStaticMapKeepsKeyServerSide(t *testing.T) {
	var seenKey string
	client := NewClientForTest("secret-amap-key", "http://go.test", "https://amap.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			seenKey = request.URL.Query().Get("key")
			return response(http.StatusOK, "image/png", "png-data"), nil
		}),
	})
	mapURL, err := client.staticMapURL(staticMapRequest{Location: "120.1551,30.2741", Zoom: 11, Width: 750, Height: 400, Scale: 2})
	if err != nil {
		t.Fatalf("staticMapURL: %v", err)
	}
	parsed, err := url.Parse(mapURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	media, err := client.ProxyStaticMap(context.Background(), parsed.Query().Get("token"))
	if err != nil {
		t.Fatalf("ProxyStaticMap: %v", err)
	}
	if seenKey != "secret-amap-key" || string(media.Body) != "png-data" {
		t.Fatalf("upstream key/body = %q/%q", seenKey, media.Body)
	}
	if _, err := client.ProxyStaticMap(context.Background(), parsed.Query().Get("token")+"x"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered token error = %v, want ErrInvalidToken", err)
	}
}

func TestPhotoProxyRejectsUntrustedSourcesAndNonImages(t *testing.T) {
	if err := validatePhotoSource("http://aos-cdn-image.amap.com/photo.jpg"); err == nil {
		t.Fatal("http photo source was accepted")
	}
	if err := validatePhotoSource("https://127.0.0.1/photo.jpg"); err == nil {
		t.Fatal("private host photo source was accepted")
	}
	client := NewClientForTest("secret-amap-key", "http://go.test", "https://amap.test", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(http.StatusOK, "text/html", "not-an-image"), nil
		}),
	})
	photoURL, err := client.photoURL("https://aos-cdn-image.amap.com/photo.jpg")
	if err != nil {
		t.Fatalf("photoURL: %v", err)
	}
	parsed, _ := url.Parse(photoURL)
	if _, err := client.ProxyPhoto(context.Background(), parsed.Query().Get("token")); !errors.Is(err, ErrUpstream) {
		t.Fatalf("ProxyPhoto error = %v, want ErrUpstream", err)
	}
}

func TestEnrichPOIsReturnsProxyMediaAndDayMap(t *testing.T) {
	client := NewClientForTest("secret-amap-key", "http://go.test", "https://amap.test", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/v3/place/text":
				return response(http.StatusOK, "application/json", `{"status":"1","pois":[{"name":"西湖","location":"120.1400,30.2400","address":"西湖区","photos":[{"url":"https://aos-cdn-image.amap.com/west-lake.jpg"}]}]}`), nil
			default:
				return nil, errors.New("unexpected endpoint")
			}
		}),
	})
	payload, err := client.EnrichPOIs(context.Background(), "杭州", []domain.POI{{Day: 1, Name: "西湖"}})
	if err != nil {
		t.Fatalf("EnrichPOIs: %v", err)
	}
	if len(payload.POIs) != 1 || payload.POIs[0].Location == "" || !strings.Contains(payload.POIs[0].Photo, "/api/poi-photo?") || !strings.Contains(payload.POIs[0].MapThumb, "/api/maps/static?") {
		t.Fatalf("unexpected POI payload: %+v", payload)
	}
	if payload.Maps["1"] == "" || strings.Contains(payload.Maps["1"], "secret-amap-key") {
		t.Fatalf("unexpected day maps: %+v", payload.Maps)
	}
}
