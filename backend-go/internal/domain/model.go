package domain

// POI is the frontend-compatible representation of one itinerary stop. The
// planner initially fills the first five fields from the legacy LLM protocol;
// the map provider may then enrich the remaining fields.
type POI struct {
	Day         int    `json:"day"`
	Name        string `json:"name"`
	Duration    string `json:"duration"`
	Price       string `json:"price"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Address     string `json:"address"`
	Photo       string `json:"photo"`
	MapThumb    string `json:"map_thumb,omitempty"`
}

// ItineraryPOIs keeps the legacy POI event shape while allowing the provider
// to generate server-side map URLs without disclosing its API key.
type ItineraryPOIs struct {
	POIs []POI             `json:"pois"`
	Maps map[string]string `json:"maps"`
}

// ImageEntry and ImageSearchResult keep GET /api/images compatible with the
// React client while all image URLs remain server-controlled proxies.
type ImageEntry struct {
	URL    string `json:"url"`
	Thumb  string `json:"thumb"`
	Alt    string `json:"alt"`
	Credit string `json:"credit"`
	Link   string `json:"link"`
}

type ImageSearchResult struct {
	Images     []ImageEntry `json:"images"`
	ScenicPool []ImageEntry `json:"scenic_pool"`
	Location   *string      `json:"location"`
}

// MediaResponse deliberately carries only validated image content. Provider
// headers, redirect locations, and upstream URLs are never forwarded.
type MediaResponse struct {
	ContentType string
	Body        []byte
}

// Transport modes accepted by the legacy Python API.
const (
	TransportSelfDrive = "自驾"
	TransportPublic    = "公共交通"
	TransportMixed     = "混合"
)

// Travel styles accepted by the legacy Python API.
const (
	TravelStyleFood       = "美食"
	TravelStyleCulture    = "文化"
	TravelStyleScenery    = "景色"
	TravelStyleShopping   = "购物"
	TravelStyleRelaxation = "休闲"
	TravelStyleAdventure  = "探险"
)

// Budget levels accepted by the legacy Python API.
const (
	BudgetEconomy = "经济"
	BudgetMiddle  = "中等"
	BudgetLuxury  = "豪华"
)

// PlanRequest is the normalized request used by the planner. HTTP DTOs apply
// defaults before constructing this value so that an explicit zero is still
// rejected by validation rather than being mistaken for an omitted field.
type PlanRequest struct {
	Origin      string
	Destination string
	StartDate   string
	EndDate     string
	Transport   string
	Preferences []string
	People      int
	BudgetLevel *string
}

// RegenerateRequest contains a normalized plan request and the module-specific
// context sent by the existing frontend.
type RegenerateRequest struct {
	PlanRequest
	Module   string
	Feedback string
	Context  map[string]any
}

// PlanResponse keeps the five legacy response fields. The fake planner leaves
// itinerary_pois empty; the field remains available for the later POI adapter.
type PlanResponse struct {
	Success       bool    `json:"success"`
	Destination   string  `json:"destination"`
	Weather       string  `json:"weather"`
	Accommodation string  `json:"accommodation"`
	Itinerary     string  `json:"itinerary"`
	Budget        string  `json:"budget"`
	ItineraryPOIs any     `json:"itinerary_pois"`
	Error         *string `json:"error"`
}

// StageEvent is the JSON payload written after the data: prefix in the SSE
// protocol. Trace events use TraceID instead of Content, matching the Python
// service's current wire shape.
type StageEvent struct {
	Stage   string `json:"stage,omitempty"`
	Content any    `json:"content,omitempty"`
	TraceID string `json:"trace_id,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    string `json:"code,omitempty"`
}
