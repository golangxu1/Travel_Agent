package planner

import (
	"context"
	"errors"
	"time"

	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/trace"
)

type traceRecorder struct {
	repo  trace.Repository
	id    string
	start time.Time
}

func beginTrace(ctx context.Context, repo trace.Repository, req domain.PlanRequest) *traceRecorder {
	if repo == nil {
		return nil
	}
	id, err := repo.Create(ctx, req.Origin, req.Destination)
	if err != nil {
		return nil
	}
	return &traceRecorder{repo: repo, id: id, start: time.Now()}
}

func (r *traceRecorder) traceID(fallback string) string {
	if r == nil || r.id == "" {
		return fallback
	}
	return r.id
}

func (r *traceRecorder) span(module string, phase int, started time.Time, output string, status string) {
	if r == nil {
		return
	}
	ctx := context.Background()
	_ = r.repo.AddSpan(ctx, r.id, trace.Span{
		AgentName: module, AgentLabel: module, Phase: phase,
		StartOffsetMS: started.Sub(r.start).Milliseconds(), DurationMS: time.Since(started).Milliseconds(),
		OutputChars: len([]rune(output)), Status: status,
	})
}

func (r *traceRecorder) finish(err error) {
	if r == nil {
		return
	}
	status := "success"
	failure := ""
	if err != nil {
		status = "failed"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = "cancelled"
			failure = "request_cancelled"
		} else {
			failure = "planner_failed"
		}
	}
	_ = r.repo.Finish(context.Background(), r.id, time.Since(r.start).Milliseconds(), status, failure)
}
