package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientOpenAICompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header was not set")
		}
		var request openAIRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "test-model" || len(request.Messages) != 2 {
			t.Fatalf("unexpected request: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"## mock"}}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`))
	}))
	defer server.Close()

	client := NewHTTPClient("openai", server.URL, "test-key", time.Second)
	response, err := client.Complete(context.Background(), Request{Model: "test-model", System: "system", User: "user", MaxOutputTokens: 128})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if response.Text != "## mock" || response.InputTokens != 11 || response.OutputTokens != 7 {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestHTTPClientAnthropicResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %s, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			t.Fatalf("anthropic headers were not set")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"## anthropic mock"}],"usage":{"input_tokens":13,"output_tokens":5}}`))
	}))
	defer server.Close()

	client := NewHTTPClient("anthropic", server.URL, "test-key", time.Second)
	response, err := client.Complete(context.Background(), Request{Model: "claude-test", System: "system", User: "user", MaxOutputTokens: 128})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if response.Text != "## anthropic mock" || response.InputTokens != 13 || response.OutputTokens != 5 {
		t.Fatalf("unexpected response: %+v", response)
	}
}
