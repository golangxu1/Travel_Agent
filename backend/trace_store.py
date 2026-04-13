# -*- coding: utf-8 -*-
# backend/trace_store.py

import sqlite3
import uuid
from datetime import datetime
from typing import List, Optional

DB_PATH = "trace.db"


def _conn():
    c = sqlite3.connect(DB_PATH)
    c.row_factory = sqlite3.Row
    return c


def init_db():
    c = _conn()
    c.executescript("""
        CREATE TABLE IF NOT EXISTS traces (
            id              TEXT PRIMARY KEY,
            origin          TEXT,
            destination     TEXT,
            created_at      TEXT,
            total_duration_ms INTEGER DEFAULT 0
        );
        CREATE TABLE IF NOT EXISTS trace_spans (
            id              INTEGER PRIMARY KEY AUTOINCREMENT,
            trace_id        TEXT REFERENCES traces(id),
            agent_name      TEXT,
            agent_label     TEXT,
            phase           INTEGER,
            start_offset_ms INTEGER,
            duration_ms     INTEGER,
            input_tokens    INTEGER,
            output_tokens   INTEGER,
            tool_calls_count INTEGER,
            output_chars    INTEGER,
            status          TEXT
        );
    """)
    c.commit()
    c.close()


def create_trace(origin: str, destination: str) -> str:
    trace_id = str(uuid.uuid4())[:8]
    c = _conn()
    c.execute(
        "INSERT INTO traces (id, origin, destination, created_at) VALUES (?, ?, ?, ?)",
        (trace_id, origin, destination, datetime.now().isoformat(timespec="seconds")),
    )
    c.commit()
    c.close()
    return trace_id


def add_span(trace_id: str, **kw):
    c = _conn()
    c.execute(
        """INSERT INTO trace_spans
           (trace_id, agent_name, agent_label, phase,
            start_offset_ms, duration_ms,
            input_tokens, output_tokens, tool_calls_count,
            output_chars, status)
           VALUES (?,?,?,?,?,?,?,?,?,?,?)""",
        (
            trace_id,
            kw.get("agent_name"),
            kw.get("agent_label"),
            kw.get("phase"),
            kw.get("start_offset_ms"),
            kw.get("duration_ms"),
            kw.get("input_tokens"),
            kw.get("output_tokens"),
            kw.get("tool_calls_count"),
            kw.get("output_chars"),
            kw.get("status"),
        ),
    )
    c.commit()
    c.close()


def finish_trace(trace_id: str, total_duration_ms: int):
    c = _conn()
    c.execute(
        "UPDATE traces SET total_duration_ms = ? WHERE id = ?",
        (total_duration_ms, trace_id),
    )
    c.commit()
    c.close()


def get_traces(limit: int = 20) -> List[dict]:
    c = _conn()
    rows = c.execute(
        "SELECT * FROM traces ORDER BY created_at DESC LIMIT ?", (limit,)
    ).fetchall()
    c.close()
    return [dict(r) for r in rows]


def get_trace_detail(trace_id: str) -> Optional[dict]:
    c = _conn()
    trace = c.execute("SELECT * FROM traces WHERE id = ?", (trace_id,)).fetchone()
    if not trace:
        c.close()
        return None
    spans = c.execute(
        "SELECT * FROM trace_spans WHERE trace_id = ? ORDER BY phase, start_offset_ms",
        (trace_id,),
    ).fetchall()
    c.close()
    return {"trace": dict(trace), "spans": [dict(s) for s in spans]}


init_db()
