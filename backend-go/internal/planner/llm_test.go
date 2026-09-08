package planner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/provider/llm"
	"travel-agent/backend-go/internal/trace"
)

type recordingLLM struct {
	mu    sync.Mutex
	calls []string
}

func (c *recordingLLM) Complete(_ context.Context, request llm.Request) (llm.Response, error) {
	c.mu.Lock()
	c.calls = append(c.calls, request.User)
	c.mu.Unlock()

	switch {
	case strings.Contains(request.System, "天气顾问"):
		return llm.Response{Text: "## 天气概况\n晴朗", InputTokens: 10, OutputTokens: 4}, nil
	case strings.Contains(request.System, "目的地专家"):
		return llm.Response{Text: "## 城市印象\n杭州", InputTokens: 11, OutputTokens: 5}, nil
	case strings.Contains(request.System, "住宿顾问"):
		return llm.Response{Text: "## 住宿区域推荐\n西湖周边", InputTokens: 12, OutputTokens: 6}, nil
	case strings.Contains(request.System, "行程规划师"):
		return llm.Response{Text: "## Day 1 · 湖畔\n09:00 西湖\n---POIS---\n1|西湖|120分钟|免费|湖景", InputTokens: 13, OutputTokens: 7}, nil
	case strings.Contains(request.System, "预算顾问"):
		return llm.Response{Text: "## 总计\n¥100", InputTokens: 14, OutputTokens: 8}, nil
	default:
		return llm.Response{Text: "## mock"}, nil
	}
}

type plannerTraceRepo struct{ spans []trace.Span }

func (r *plannerTraceRepo) Create(context.Context, string, string) (string, error) {
	return "trace-test", nil
}
func (r *plannerTraceRepo) AddSpan(_ context.Context, _ string, span trace.Span) error {
	r.spans = append(r.spans, span)
	return nil
}
func (r *plannerTraceRepo) Finish(context.Context, string, int64, string, string) error { return nil }
func (r *plannerTraceRepo) List(context.Context, int) ([]trace.Trace, error)            { return nil, nil }
func (r *plannerTraceRepo) Get(context.Context, string) (*trace.Detail, error) {
	return nil, trace.ErrNotFound
}

func TestLLMPlannerPersistsProviderTokenUsage(t *testing.T) {
	repo := &plannerTraceRepo{}
	p := NewLLMPlanner(&recordingLLM{}, "test-model", 256, time.Second)
	p.TraceRepository = repo
	stream, err := p.Stream(context.Background(), testPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	for range stream.Events {
	}
	if err := receiveStreamError(stream); err != nil {
		t.Fatal(err)
	}
	if len(repo.spans) != 5 {
		t.Fatalf("spans = %d, want 5", len(repo.spans))
	}
	input, output := 0, 0
	for _, span := range repo.spans {
		input += span.InputTokens
		output += span.OutputTokens
	}
	if input != 60 || output != 30 {
		t.Fatalf("tokens = %d/%d, want 60/30", input, output)
	}
}

func testPlanRequest() domain.PlanRequest {
	return domain.PlanRequest{
		Origin: "上海", Destination: "杭州", StartDate: "2026-10-01", EndDate: "2026-10-03",
		Transport: domain.TransportMixed, Preferences: []string{domain.TravelStyleScenery}, People: 2,
	}
}

func TestLLMPlannerStreamPreservesCompatibilityOrder(t *testing.T) {
	client := &recordingLLM{}
	planner := NewLLMPlanner(client, "test-model", 256, time.Second)
	stream, err := planner.Stream(context.Background(), testPlanRequest())
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var stages []string
	for event := range stream.Events {
		stages = append(stages, event.Stage)
	}
	if err := receiveStreamError(stream); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	want := []string{"weather", "destination", "accommodation", "itinerary", "itinerary_pois", "budget", "trace"}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	client.mu.Lock()
	callCount := len(client.calls)
	client.mu.Unlock()
	if callCount != 5 {
		t.Fatalf("llm calls = %d, want 5", callCount)
	}
}

func TestLLMPlannerRegenerateItineraryEmitsPOIs(t *testing.T) {
	client := &recordingLLM{}
	planner := NewLLMPlanner(client, "test-model", 256, time.Second)
	request := domain.RegenerateRequest{PlanRequest: testPlanRequest(), Module: "itinerary", Feedback: "减少步行", Context: map[string]any{"weather": "晴朗", "destination": "杭州"}}
	stream, err := planner.RegenerateStream(context.Background(), request)
	if err != nil {
		t.Fatalf("regenerate stream: %v", err)
	}
	var stages []string
	for event := range stream.Events {
		stages = append(stages, event.Stage)
	}
	if err := receiveStreamError(stream); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if strings.Join(stages, ",") != "itinerary,itinerary_pois" {
		t.Fatalf("stages = %v", stages)
	}
}
