CREATE TABLE IF NOT EXISTS traces (
    id TEXT PRIMARY KEY,
    origin TEXT NOT NULL,
    destination TEXT NOT NULL,
    created_at TEXT NOT NULL,
    total_duration_ms INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'running',
    error TEXT
);

CREATE TABLE IF NOT EXISTS trace_spans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trace_id TEXT NOT NULL REFERENCES traces(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    agent_label TEXT NOT NULL DEFAULT '',
    phase INTEGER NOT NULL DEFAULT 0,
    start_offset_ms INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    tool_calls_count INTEGER NOT NULL DEFAULT 0,
    output_chars INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'success'
);

CREATE INDEX IF NOT EXISTS idx_traces_created_at ON traces(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trace_spans_trace_id ON trace_spans(trace_id, phase, start_offset_ms);
