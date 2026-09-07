package planner

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"travel-agent/backend-go/internal/agent"
	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/provider/llm"
)

// LLMPlanner implements the same observable three-phase flow as the Python
// service. It emits completed module events in compatibility order while the
// first phase runs concurrently.
type LLMPlanner struct {
	Client          llm.Client
	Model           string
	MaxOutputTokens int
	StageTimeout    time.Duration
	POIEnricher     POIEnricher
}

func NewLLMPlanner(client llm.Client, model string, maxOutputTokens int, stageTimeout time.Duration, enrichers ...POIEnricher) *LLMPlanner {
	if maxOutputTokens <= 0 {
		maxOutputTokens = 4096
	}
	if stageTimeout <= 0 {
		stageTimeout = 120 * time.Second
	}
	planner := &LLMPlanner{Client: client, Model: model, MaxOutputTokens: maxOutputTokens, StageTimeout: stageTimeout}
	if len(enrichers) > 0 {
		planner.POIEnricher = enrichers[0]
	}
	return planner
}

func (p *LLMPlanner) Plan(ctx context.Context, req domain.PlanRequest) (domain.PlanResponse, error) {
	stream, err := p.Stream(ctx, req)
	if err != nil {
		return domain.PlanResponse{}, err
	}
	response := domain.PlanResponse{Success: true}
	for event := range stream.Events {
		if event.Stage == "trace" {
			continue
		}
		if event.Stage == "itinerary_pois" {
			response.ItineraryPOIs = event.Content
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

func (p *LLMPlanner) Stream(ctx context.Context, req domain.PlanRequest) (*EventStream, error) {
	if p.Client == nil {
		return nil, errors.New("llm client is not configured")
	}
	if err := domain.ValidatePlan(req); err != nil {
		return nil, err
	}
	events := make(chan domain.StageEvent)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		if err := p.runPlan(ctx, req, events); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()
	return &EventStream{Events: events, Err: errs}, nil
}

func (p *LLMPlanner) runPlan(ctx context.Context, req domain.PlanRequest, events chan<- domain.StageEvent) error {
	type result struct {
		module  string
		content string
		err     error
	}
	modules := []string{"weather", "destination", "accommodation"}
	results := make(chan result, len(modules))
	var wg sync.WaitGroup
	for _, module := range modules {
		module := module
		wg.Add(1)
		go func() {
			defer wg.Done()
			content, err := p.complete(ctx, module, req, "", "")
			results <- result{module: module, content: content, err: err}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	phaseOne := make(map[string]string, len(modules))
	for result := range results {
		if result.err != nil {
			return result.err
		}
		phaseOne[result.module] = result.content
	}
	for _, module := range modules {
		if err := emit(ctx, events, domain.StageEvent{Stage: module, Content: phaseOne[module]}); err != nil {
			return err
		}
	}

	itineraryContext := "天气\n" + agent.TrimContext(phaseOne["weather"], 300) + "\n\n目的地\n" + agent.TrimContext(phaseOne["destination"], 500)
	itinerary, err := p.complete(ctx, "itinerary", req, itineraryContext, "")
	if err != nil {
		return err
	}
	itineraryMarkdown, pois := parseItinerary(itinerary)
	if itineraryMarkdown == "" {
		itineraryMarkdown = itinerary
	}
	if err := emit(ctx, events, domain.StageEvent{Stage: "itinerary", Content: itineraryMarkdown}); err != nil {
		return err
	}
	if len(pois) > 0 {
		payload := p.enrichPOIs(ctx, req.Destination, pois)
		if err := emit(ctx, events, domain.StageEvent{Stage: "itinerary_pois", Content: payload}); err != nil {
			return err
		}
	}

	budgetContext := "行程\n" + agent.TrimContext(itineraryMarkdown, 800) + "\n\n住宿\n" + agent.TrimContext(phaseOne["accommodation"], 500)
	budget, err := p.complete(ctx, "budget", req, budgetContext, "")
	if err != nil {
		return err
	}
	if err := emit(ctx, events, domain.StageEvent{Stage: "budget", Content: budget}); err != nil {
		return err
	}
	return emit(ctx, events, domain.StageEvent{Stage: "trace", TraceID: newTraceID()})
}

func (p *LLMPlanner) RegenerateStream(ctx context.Context, req domain.RegenerateRequest) (*EventStream, error) {
	if p.Client == nil {
		return nil, errors.New("llm client is not configured")
	}
	if err := domain.ValidateRegenerate(req); err != nil {
		return nil, err
	}
	events := make(chan domain.StageEvent, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		contextText := contextValue(req.Context, req.Module)
		if req.Module == "itinerary" {
			contextText = "天气\n" + contextValue(req.Context, "weather") + "\n\n目的地\n" + contextValue(req.Context, "destination")
		} else if req.Module == "budget" {
			contextText = "行程\n" + contextValue(req.Context, "itinerary") + "\n\n住宿\n" + contextValue(req.Context, "accommodation")
		}
		content, err := p.complete(ctx, req.Module, req.PlanRequest, contextText, req.Feedback)
		if err != nil {
			if ctx.Err() == nil {
				errs <- err
			}
			return
		}
		if req.Module == "itinerary" {
			markdown, pois := parseItinerary(content)
			if markdown != "" {
				content = markdown
			}
			if err := emit(ctx, events, domain.StageEvent{Stage: "itinerary", Content: content}); err != nil {
				return
			}
			if len(pois) > 0 {
				_ = emit(ctx, events, domain.StageEvent{Stage: "itinerary_pois", Content: p.enrichPOIs(ctx, req.Destination, pois)})
			}
		} else {
			_ = emit(ctx, events, domain.StageEvent{Stage: req.Module, Content: content})
		}
	}()
	return &EventStream{Events: events, Err: errs}, nil
}

func (p *LLMPlanner) complete(ctx context.Context, module string, req domain.PlanRequest, contextText string, feedback string) (string, error) {
	stageCtx, cancel := context.WithTimeout(ctx, p.StageTimeout)
	defer cancel()
	system, user := agent.Prompt(module, req, contextText, feedback)
	response, err := p.Client.Complete(stageCtx, llm.Request{Model: p.Model, System: system, User: user, MaxOutputTokens: p.MaxOutputTokens})
	if err != nil {
		return "", fmt.Errorf("module %s failed: %w", module, err)
	}
	if strings.TrimSpace(response.Text) == "" {
		return "", fmt.Errorf("module %s returned empty content", module)
	}
	return stripThinking(response.Text), nil
}

func emit(ctx context.Context, events chan<- domain.StageEvent, event domain.StageEvent) error {
	select {
	case events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseItinerary(raw string) (string, []domain.POI) {
	parts := strings.SplitN(raw, "---POIS---", 2)
	if len(parts) != 2 {
		return raw, nil
	}
	var pois []domain.POI
	for _, line := range strings.Split(parts[1], "\n") {
		columns := strings.SplitN(strings.TrimSpace(line), "|", 5)
		if len(columns) != 5 {
			continue
		}
		day, err := strconv.Atoi(strings.TrimSpace(columns[0]))
		if err != nil || day < 1 || strings.TrimSpace(columns[1]) == "" {
			continue
		}
		pois = append(pois, domain.POI{Day: day, Name: strings.TrimSpace(columns[1]), Duration: strings.TrimSpace(columns[2]), Price: strings.TrimSpace(columns[3]), Description: strings.TrimSpace(columns[4])})
	}
	return strings.TrimSpace(parts[0]), pois
}

func (p *LLMPlanner) enrichPOIs(ctx context.Context, city string, pois []domain.POI) domain.ItineraryPOIs {
	payload := domain.ItineraryPOIs{POIs: pois, Maps: map[string]string{}}
	if p.POIEnricher == nil {
		return payload
	}
	enriched, err := p.POIEnricher.EnrichPOIs(ctx, city, pois)
	if err != nil {
		return payload
	}
	if enriched.POIs == nil {
		enriched.POIs = pois
	}
	if enriched.Maps == nil {
		enriched.Maps = map[string]string{}
	}
	return enriched
}

func stripThinking(content string) string {
	content = strings.TrimSpace(content)
	if index := strings.Index(content, "## "); index > 0 {
		return strings.TrimSpace(content[index:])
	}
	return content
}

func contextValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	return agent.TrimContext(fmt.Sprint(value), 800)
}
