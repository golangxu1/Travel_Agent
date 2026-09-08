package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowsConfiguredFrontend(t *testing.T) {
	server := NewServer(nil)
	server.AllowedOrigins = []string{"http://localhost:5173"}

	req := httptest.NewRequest(http.MethodOptions, "/api/plan/stream", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow origin = %q", got)
	}
}

func TestCORSRejectsUnknownPreflightOrigin(t *testing.T) {
	server := NewServer(nil)
	server.AllowedOrigins = []string{"http://localhost:5173"}
	req := httptest.NewRequest(http.MethodOptions, "/api/plan/stream", nil)
	req.Header.Set("Origin", "https://untrusted.example")
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || recorder.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("status/allow-origin = %d/%q", recorder.Code, recorder.Header().Get("Access-Control-Allow-Origin"))
	}
}
