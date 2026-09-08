package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"travel-agent/backend-go/internal/planner"
	"travel-agent/backend-go/internal/trace"
)

type traceAPIRepo struct{}

func (traceAPIRepo) Create(context.Context, string, string) (string, error)      { return "id", nil }
func (traceAPIRepo) AddSpan(context.Context, string, trace.Span) error           { return nil }
func (traceAPIRepo) Finish(context.Context, string, int64, string, string) error { return nil }
func (traceAPIRepo) List(context.Context, int) ([]trace.Trace, error) {
	return []trace.Trace{{ID: "id", Origin: "北京", Destination: "杭州", Status: "success"}}, nil
}
func (traceAPIRepo) Get(context.Context, string) (*trace.Detail, error) {
	return &trace.Detail{Trace: trace.Trace{ID: "id", Origin: "北京", Destination: "杭州"}, Spans: []trace.Span{}}, nil
}

func TestTraceRoutesKeepLegacyShapes(t *testing.T) {
	server := NewServer(planner.NewFakePlanner(), traceAPIRepo{}).Routes()
	list := httptest.NewRecorder()
	server.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/traces?limit=20", nil))
	if list.Code != http.StatusOK || list.Body.String() == "" || !strings.Contains(list.Body.String(), `"traces"`) {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	detail := httptest.NewRecorder()
	server.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/traces/id", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"trace"`) || !strings.Contains(detail.Body.String(), `"spans"`) {
		t.Fatalf("detail = %d %s", detail.Code, detail.Body.String())
	}
	bad := httptest.NewRecorder()
	server.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/traces/id/extra", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad id status = %d", bad.Code)
	}
}
