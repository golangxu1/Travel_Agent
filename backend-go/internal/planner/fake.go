package planner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/trace"
)

// FakePlanner is deterministic and has no network or LLM dependency. A stage
// delay can be supplied by tests to exercise streaming and cancellation.
type FakePlanner struct {
	StageDelay      time.Duration
	TraceRepository trace.Repository
}

func NewFakePlanner() *FakePlanner {
	return &FakePlanner{}
}

func (p *FakePlanner) Plan(ctx context.Context, req domain.PlanRequest) (domain.PlanResponse, error) {
	stream, err := p.Stream(ctx, req)
	if err != nil {
		return domain.PlanResponse{}, err
	}

	var response domain.PlanResponse
	response.Success = true
	for event := range stream.Events {
		if event.Stage == "trace" || event.Stage == "itinerary_pois" {
			continue
		}
		content, ok := event.Content.(string)
		if !ok {
			continue
		}
		switch event.Stage {
		case "weather":
			response.Weather = content
		case "destination":
			response.Destination = content
		case "accommodation":
			response.Accommodation = content
		case "itinerary":
			response.Itinerary = content
		case "budget":
			response.Budget = content
		}
	}
	if err := receiveStreamError(stream); err != nil {
		return domain.PlanResponse{}, err
	}
	return response, nil
}

func (p *FakePlanner) Stream(ctx context.Context, req domain.PlanRequest) (*EventStream, error) {
	if err := domain.ValidatePlan(req); err != nil {
		return nil, err
	}

	events := make(chan domain.StageEvent)
	errs := make(chan error, 1)
	recorder := beginTrace(ctx, p.TraceRepository, req)
	traceID := recorder.traceID(newTraceID())
	stages := []domain.StageEvent{
		{Stage: "weather", Content: fmt.Sprintf("## 天气\n%s 至 %s 的天气概览（模拟数据）", req.StartDate, req.EndDate)},
		{Stage: "destination", Content: fmt.Sprintf("## 目的地\n%s 的目的地概览（模拟数据）", req.Destination)},
		{Stage: "accommodation", Content: fmt.Sprintf("## 住宿\n%s 的住宿建议（模拟数据）", req.Destination)},
		{Stage: "itinerary", Content: fmt.Sprintf("## 行程\n%s：%d 人，按 %d 天生成的行程（模拟数据）", req.Destination, req.People, tripDays(req))},
		{Stage: "budget", Content: fmt.Sprintf("## 预算\n%s 的预算估算（模拟数据）", req.Destination)},
		{Stage: "trace", TraceID: traceID},
	}

	go func() {
		defer close(events)
		defer close(errs)
		var streamErr error
		defer func() { recorder.finish(streamErr) }()
		for _, event := range stages {
			started := time.Now()
			if err := p.wait(ctx); err != nil {
				streamErr = err
				errs <- err
				return
			}
			select {
			case events <- event:
			case <-ctx.Done():
				streamErr = ctx.Err()
				errs <- ctx.Err()
				return
			}
			if event.Stage != "trace" {
				phase := 1
				if event.Stage == "itinerary" {
					phase = 2
				} else if event.Stage == "budget" {
					phase = 3
				}
				recorder.span(event.Stage, phase, started, fmt.Sprint(event.Content), "success", 0, 0)
			}
		}
	}()
	return &EventStream{Events: events, Err: errs}, nil
}

func (p *FakePlanner) RegenerateStream(ctx context.Context, req domain.RegenerateRequest) (*EventStream, error) {
	if err := domain.ValidateRegenerate(req); err != nil {
		return nil, err
	}

	events := make(chan domain.StageEvent, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		if err := p.wait(ctx); err != nil {
			errs <- err
			return
		}
		content := fmt.Sprintf("## %s\n基于追加要求重新生成（模拟数据）：%s", req.Module, req.Feedback)
		event := domain.StageEvent{Stage: req.Module, Content: content}
		select {
		case events <- event:
		case <-ctx.Done():
			errs <- ctx.Err()
		}
	}()
	return &EventStream{Events: events, Err: errs}, nil
}

func (p *FakePlanner) wait(ctx context.Context) error {
	if p.StageDelay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(p.StageDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func receiveStreamError(stream *EventStream) error {
	if stream == nil || stream.Err == nil {
		return nil
	}
	for err := range stream.Err {
		if err != nil {
			return err
		}
	}
	return nil
}

func tripDays(req domain.PlanRequest) int {
	start, startErr := time.Parse("2006-01-02", req.StartDate)
	end, endErr := time.Parse("2006-01-02", req.EndDate)
	if startErr != nil || endErr != nil || end.Before(start) {
		return 1
	}
	return int(end.Sub(start).Hours()/24) + 1
}

func newTraceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}
