package planner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/provider/llm"
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
		return llm.Response{Text: "## 天气概况\n晴朗"}, nil
	case strings.Contains(request.System, "目的地专家"):
		return llm.Response{Text: "## 城市印象\n杭州"}, nil
	case strings.Contains(request.System, "住宿顾问"):
		return llm.Response{Text: "## 住宿区域推荐\n西湖周边"}, nil
	case strings.Contains(request.System, "行程规划师"):
		return llm.Response{Text: "## Day 1 · 湖畔\n09:00 西湖\n---POIS---\n1|西湖|120分钟|免费|湖景"}, nil
	case strings.Contains(request.System, "预算顾问"):
		return llm.Response{Text: "## 总计\n¥100"}, nil
	default:
		return llm.Response{Text: "## mock"}, nil
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
