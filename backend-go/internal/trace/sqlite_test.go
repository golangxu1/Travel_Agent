package trace

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSQLiteRepositoryRunsMigrationsAndPersistsLifecycle(t *testing.T) {
	repo, err := OpenSQLite(filepath.Join(t.TempDir(), "traces.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()
	id, err := repo.Create(ctx, "北京", "杭州")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AddSpan(ctx, id, Span{AgentName: "weather", AgentLabel: "天气", Phase: 1, OutputChars: 10, Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Finish(ctx, id, 42, "success", ""); err != nil {
		t.Fatal(err)
	}

	detail, err := repo.Get(ctx, id)
	if err != nil || detail.Trace.Status != "success" || detail.Trace.TotalDurationMS != 42 || len(detail.Spans) != 1 {
		t.Fatalf("detail = %#v, err = %v", detail, err)
	}
	items, err := repo.List(ctx, 20)
	if err != nil || len(items) != 1 || items[0].ID != id {
		t.Fatalf("list = %#v, err = %v", items, err)
	}
	if _, err := repo.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing trace error = %v", err)
	}
}

func TestSQLiteRepositoryEnforcesForeignKeys(t *testing.T) {
	repo, err := OpenSQLite(filepath.Join(t.TempDir(), "traces.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.AddSpan(context.Background(), "missing", Span{AgentName: "weather"}); err == nil {
		t.Fatal("expected foreign-key error")
	}
}
