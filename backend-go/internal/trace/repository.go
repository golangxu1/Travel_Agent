package trace

import "context"

// Repository is the persistence boundary for planner execution traces.
// Implementations must keep the API independent from the storage engine so
// SQLite can later be replaced by PostgreSQL without changing planner code.
type Repository interface {
	Create(context.Context, string, string) (string, error)
	AddSpan(context.Context, string, Span) error
	Finish(context.Context, string, int64, string, string) error
	List(context.Context, int) ([]Trace, error)
	Get(context.Context, string) (*Detail, error)
}

type Trace struct {
	ID              string `json:"id"`
	Origin          string `json:"origin"`
	Destination     string `json:"destination"`
	CreatedAt       string `json:"created_at"`
	TotalDurationMS int64  `json:"total_duration_ms"`
	Status          string `json:"status"`
	Error           string `json:"error,omitempty"`
}

type Span struct {
	AgentName      string `json:"agent_name"`
	AgentLabel     string `json:"agent_label"`
	Phase          int    `json:"phase"`
	StartOffsetMS  int64  `json:"start_offset_ms"`
	DurationMS     int64  `json:"duration_ms"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
	ToolCallsCount int    `json:"tool_calls_count"`
	OutputChars    int    `json:"output_chars"`
	Status         string `json:"status"`
}

type Detail struct {
	Trace Trace  `json:"trace"`
	Spans []Span `json:"spans"`
}
