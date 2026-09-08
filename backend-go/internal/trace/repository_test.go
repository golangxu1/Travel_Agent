package trace

import (
	"context"
	"errors"
	"testing"
)

type memoryRepository struct {
	created bool
	trace   Trace
	spans   []Span
}

func (m *memoryRepository) Create(_ context.Context, origin, destination string) (string, error) {
	m.created = true
	m.trace = Trace{ID: "trace-test", Origin: origin, Destination: destination}
	return m.trace.ID, nil
}
func (m *memoryRepository) AddSpan(_ context.Context, id string, span Span) error {
	if id != m.trace.ID {
		return errors.New("wrong trace")
	}
	m.spans = append(m.spans, span)
	return nil
}
func (m *memoryRepository) Finish(_ context.Context, id string, duration int64, status, failure string) error {
	if id != m.trace.ID {
		return ErrNotFound
	}
	m.trace.TotalDurationMS, m.trace.Status, m.trace.Error = duration, status, failure
	return nil
}
func (m *memoryRepository) List(context.Context, int) ([]Trace, error) { return []Trace{m.trace}, nil }
func (m *memoryRepository) Get(context.Context, string) (*Detail, error) {
	return &Detail{Trace: m.trace, Spans: m.spans}, nil
}

func TestRepositoryContractCanBeImplementedOutsideSQLite(t *testing.T) {
	var _ Repository = (*memoryRepository)(nil)
	repo := &memoryRepository{}
	id, err := repo.Create(context.Background(), "北京", "杭州")
	if err != nil || id != "trace-test" || !repo.created {
		t.Fatalf("create = %q, %v", id, err)
	}
	if err := repo.AddSpan(context.Background(), id, Span{AgentName: "weather", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(context.Background(), id, 12, "success", ""); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(context.Background(), id)
	if err != nil || detail.Trace.Status != "success" || len(detail.Spans) != 1 {
		t.Fatalf("detail = %#v, %v", detail, err)
	}
}
