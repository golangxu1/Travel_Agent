package planner

import (
	"context"

	"travel-agent/backend-go/internal/domain"
)

// EventStream is the cancellation-aware stream returned by planner adapters.
// Events are emitted in wire order; Err is buffered so a disconnected HTTP
// client cannot leave a producer blocked while reporting an error.
type EventStream struct {
	Events <-chan domain.StageEvent
	Err    <-chan error
}

// Planner is the application boundary between HTTP and the eventual LLM/AMap
// implementation. FakePlanner implements it for phase-0 contract testing.
type Planner interface {
	Plan(context.Context, domain.PlanRequest) (domain.PlanResponse, error)
	Stream(context.Context, domain.PlanRequest) (*EventStream, error)
	RegenerateStream(context.Context, domain.RegenerateRequest) (*EventStream, error)
}
