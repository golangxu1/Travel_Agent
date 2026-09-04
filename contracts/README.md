# Phase 0 API Contract Fixtures

These files freeze the HTTP surface that the Go service must implement before the
frontend is switched over. The contract is based on the currently running
FastAPI handlers in `backend/main.py` and the DTOs in `backend/schemas.py`.

The contract is intentionally split into a short route reference and executable
fixtures:

- [`http-contract.md`](http-contract.md) describes request/response fields,
  status codes, event ordering, and compatibility rules.
- [`fixtures/README.md`](fixtures/README.md) lists each fixture and how it is
  used by contract tests.
- `fixtures/*.json` are UTF-8 JSON request/response bodies.
- `fixtures/*.sse` are complete UTF-8 SSE response bodies. Each event uses the
  current `data: <JSON>\n\n` framing and the stream ends with `data: [DONE]\n\n`.

## Compatibility boundary

The first Go release keeps the existing paths, snake_case request fields,
Chinese enum values, five module names, and legacy SSE `stage` values. Phase 1
may still execute weather, destination, and accommodation concurrently, but
the externally visible event order is deterministic:

```text
weather -> destination -> accommodation -> itinerary
         -> itinerary_pois (optional) -> budget -> trace -> [DONE]
```

The fixtures contain safe, deterministic placeholder content. They are not
golden LLM prose tests; contract tests should validate field types, presence,
ordering, and termination.

## Legacy behavior versus Go target

Some details in the Python service are compatibility observations, not desired
security behavior:

- Python returns `success: true` even when an agent catches an exception and
  puts a failure sentence into its module content. Go must expose module-level
  failure/partial status explicitly while preserving the legacy fields for old
  clients.
- Python's validation is mostly Pydantic defaults and can turn bad date ranges
  into a 500 during prompt construction. Go must reject invalid dates, ranges,
  lengths, and enum values with a stable 4xx error.
- Python's `error` SSE value and HTTP `detail` may contain upstream exception
  text. Go errors must be sanitized; provider URLs, prompts, API keys, and
  stack traces must never appear in responses or fixtures.
- Python emits AMap URLs containing the server key and accepts arbitrary image
  proxy URLs. The fixture uses redacted paths (`/api/media/...`) only. Go must
  keep keys server-side and enforce an allowlisted, SSRF-safe media proxy.
- Python parses `---POIS---` pipe-delimited text. The legacy response shape is
  frozen here for the first cut, while the Go planner should prefer a validated,
  versioned structured POI payload and retain the text parser only as a
  temporary fallback.
- Current trace IDs are short strings (the Python store truncates UUIDs to eight
  characters). The field remains a string for compatibility; Go may use a full
  UUID/ULID without changing the event or response field name.

No fixture contains a real provider key, private address, internal exception,
or user credential.
