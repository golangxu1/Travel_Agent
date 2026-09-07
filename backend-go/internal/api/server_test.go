package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/planner"
)

func planJSON() string {
	return `{"origin":"上海","destination":"杭州","start_date":"2026-10-01","end_date":"2026-10-03","transport":"混合","preferences":["景色"],"people":2}`
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test source directory")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"status":"ok"`) {
		t.Fatalf("body = %q", got)
	}
}

func TestPlanReturnsCompatibleFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/plan", strings.NewReader(planJSON()))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response domain.PlanResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success || response.Weather == "" || response.Destination == "" || response.Accommodation == "" || response.Itinerary == "" || response.Budget == "" {
		t.Fatalf("incomplete response: %+v", response)
	}
}

func TestPlanFixturePreservesLegacyNullableFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/plan", strings.NewReader(string(fixture(t, "plan-request.valid.json"))))
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{"success", "destination", "weather", "accommodation", "itinerary", "budget", "itinerary_pois", "error"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("legacy response key %q is missing: %s", key, recorder.Body.String())
		}
	}
	if got := string(payload["itinerary_pois"]); got != "null" {
		t.Fatalf("itinerary_pois = %s, want null", got)
	}
	if got := string(payload["error"]); got != "null" {
		t.Fatalf("error = %s, want null", got)
	}
}

func TestPlanAppliesLegacyDefaultsWhenFieldsAreOmitted(t *testing.T) {
	body := `{"origin":"上海","destination":"杭州","start_date":"2026-10-01","end_date":"2026-10-03"}`
	req := httptest.NewRequest(http.MethodPost, "/api/plan", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlanRejectsUnknownField(t *testing.T) {
	body := strings.TrimSuffix(planJSON(), "}") + `,"unexpected":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/plan", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestPlanRejectsExplicitNullForDefaultedField(t *testing.T) {
	body := strings.Replace(planJSON(), `"transport":"混合"`, `"transport":null`, 1)
	req := httptest.NewRequest(http.MethodPost, "/api/plan", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", recorder.Code, recorder.Body.String())
	}
	var response requestError
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != "invalid_request" || response.Fields["transport"] == "" {
		t.Fatalf("unexpected validation response: %+v", response)
	}
}

func TestPlanStreamStageOrderAndDone(t *testing.T) {
	server := NewServer(&planner.FakePlanner{})
	req := httptest.NewRequest(http.MethodPost, "/api/plan/stream", strings.NewReader(planJSON()))
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	stages := []string{"\"stage\":\"weather\"", "\"stage\":\"destination\"", "\"stage\":\"accommodation\"", "\"stage\":\"itinerary\"", "\"stage\":\"budget\"", "\"stage\":\"trace\"", "data: [DONE]"}
	last := -1
	for _, stage := range stages {
		pos := strings.Index(body, stage)
		if pos < 0 || pos <= last {
			t.Fatalf("stage %q missing or out of order in %q", stage, body)
		}
		last = pos
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
}

func TestPlanStreamErrorMatchesFrozenFixture(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/plan/stream", strings.NewReader(planJSON()))
	recorder := httptest.NewRecorder()
	NewServer(failingStreamPlanner{}).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	want := normalizeNewlines(string(fixture(t, "error-stream.sanitized.sse")))
	got := normalizeNewlines(recorder.Body.String())
	if got != want {
		t.Fatalf("stream fixture mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRegenerateStream(t *testing.T) {
	body := strings.TrimSuffix(planJSON(), "}") + `,"module":"budget","feedback":"控制住宿开支","context":{"itinerary":"旧行程"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/plan/regenerate", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"stage":"budget"`) || !strings.Contains(recorder.Body.String(), "data: [DONE]") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestRegenerateRejectsNullContext(t *testing.T) {
	body := strings.TrimSuffix(planJSON(), "}") + `,"module":"budget","feedback":"控制住宿开支","context":null}`
	req := httptest.NewRequest(http.MethodPost, "/api/plan/regenerate", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlanStreamCancellationStopsPlanner(t *testing.T) {
	planner := &planner.FakePlanner{StageDelay: 200 * time.Millisecond}
	server := NewServer(planner)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/plan/stream", strings.NewReader(planJSON())).WithContext(ctx)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.Routes().ServeHTTP(recorder, req)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not stop after cancellation")
	}
}

func TestImagesReturnsCompatibleMediaShape(t *testing.T) {
	location := "120.1551,30.2741"
	media := &fakeMedia{images: domain.ImageSearchResult{
		Images:     []domain.ImageEntry{{URL: "http://go.test/api/maps/static?token=map", Alt: "杭州 - 城市全景"}},
		ScenicPool: []domain.ImageEntry{{URL: "http://go.test/api/poi-photo?token=photo", Alt: "西湖"}},
		Location:   &location,
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/images?query=%E6%9D%AD%E5%B7%9E", nil)
	recorder := httptest.NewRecorder()
	NewServer(nil, media).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"scenic_pool"`) || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if media.query != "杭州" || media.count != 4 || media.poolLimit != 24 {
		t.Fatalf("media arguments = %q/%d/%d", media.query, media.count, media.poolLimit)
	}
}

func TestImagesRejectsOutOfRangeParameters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/images?query=%E6%9D%AD%E5%B7%9E&pool_limit=25", nil)
	recorder := httptest.NewRecorder()
	NewServer(nil).Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"pool_limit"`) {
		t.Fatalf("status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
}

func TestMediaProxyOnlyAcceptsOpaqueTokens(t *testing.T) {
	media := &fakeMedia{photo: domain.MediaResponse{ContentType: "image/jpeg", Body: []byte("jpeg")}}
	server := NewServer(nil, media).Routes()

	valid := httptest.NewRecorder()
	server.ServeHTTP(valid, httptest.NewRequest(http.MethodGet, "/api/poi-photo?token=opaque-token", nil))
	if valid.Code != http.StatusOK || valid.Header().Get("X-Content-Type-Options") != "nosniff" || valid.Body.String() != "jpeg" {
		t.Fatalf("valid proxy = %d, headers=%v, body=%q", valid.Code, valid.Header(), valid.Body.String())
	}
	invalid := httptest.NewRecorder()
	server.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/poi-photo?url=https://evil.example/image.jpg", nil))
	if invalid.Code != http.StatusNotFound || media.photoToken != "opaque-token" {
		t.Fatalf("untrusted request status/token = %d/%q", invalid.Code, media.photoToken)
	}
}

func normalizeNewlines(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

type failingStreamPlanner struct{}

func (failingStreamPlanner) Plan(context.Context, domain.PlanRequest) (domain.PlanResponse, error) {
	return domain.PlanResponse{}, errors.New("simulated planner failure")
}

func (failingStreamPlanner) Stream(context.Context, domain.PlanRequest) (*planner.EventStream, error) {
	events := make(chan domain.StageEvent)
	errs := make(chan error, 1)
	errs <- errors.New("simulated planner failure")
	close(events)
	close(errs)
	return &planner.EventStream{Events: events, Err: errs}, nil
}

func (failingStreamPlanner) RegenerateStream(context.Context, domain.RegenerateRequest) (*planner.EventStream, error) {
	return failingStreamPlanner{}.Stream(context.Background(), domain.PlanRequest{})
}

type fakeMedia struct {
	images     domain.ImageSearchResult
	imageErr   error
	photo      domain.MediaResponse
	staticMap  domain.MediaResponse
	query      string
	count      int
	poolLimit  int
	photoToken string
}

func (m *fakeMedia) SearchImages(_ context.Context, query string, count, poolLimit int) (domain.ImageSearchResult, error) {
	m.query, m.count, m.poolLimit = query, count, poolLimit
	return m.images, m.imageErr
}

func (m *fakeMedia) ProxyPhoto(_ context.Context, token string) (domain.MediaResponse, error) {
	m.photoToken = token
	return m.photo, nil
}

func (m *fakeMedia) ProxyStaticMap(context.Context, string) (domain.MediaResponse, error) {
	return m.staticMap, nil
}
