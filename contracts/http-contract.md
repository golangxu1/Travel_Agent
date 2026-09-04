# HTTP Contract

This is the phase-0 compatibility contract for the travel planner. Unless a
route says otherwise, request and response bodies are JSON encoded as UTF-8.

## Shared plan request

`POST /api/plan` and `POST /api/plan/stream` accept the body in
[`plan-request.valid.json`](fixtures/plan-request.valid.json):

| Field | Type | Current accepted values / behavior |
| --- | --- | --- |
| `origin` | string | Required departure location |
| `destination` | string | Required destination |
| `start_date` | string | `YYYY-MM-DD` text in Python; Go must parse it |
| `end_date` | string | `YYYY-MM-DD` text in Python; Go must parse it and require `end >= start` |
| `transport` | string | `自驾`, `公共交通`, or `混合`; default `混合` |
| `preferences` | string[] | `美食`, `文化`, `景色`, `购物`, `休闲`, `探险`; default `[`景色`]` |
| `people` | integer | Python range `1..20`; default `2` |
| `budget_level` | string or null | `经济`, `中等`, `豪华`, or `null` (AI estimate); default `null` |

The Go service must impose explicit maximum lengths and a maximum trip span.
Those limits are a target hardening rule and are deliberately not encoded as
additional fields in the legacy request fixture.

## `POST /api/plan`

Successful response: HTTP `200`, JSON body matching
[`plan-response.valid.json`](fixtures/plan-response.valid.json).

The top-level shape is:

```json
{
  "success": true,
  "destination": "markdown string or null",
  "weather": "markdown string or null",
  "accommodation": "markdown string or null",
  "itinerary": "markdown string or null",
  "budget": "markdown string or null",
  "itinerary_pois": {"pois": [], "maps": {}} or null,
  "error": null
}
```

`itinerary_pois.pois` retains the legacy keys `day`, `name`, `duration`,
`price`, `description`, `location`, `address`, `photo`, and optional
`map_thumb`. `maps` is an object keyed by day number represented as a string.
The Go implementation may add a version field, but must not remove these keys
until the frontend contract is deliberately upgraded.

## `POST /api/plan/stream`

Successful response: HTTP `200`, `Content-Type: text/event-stream` (UTF-8).
Every event is a single `data:` line followed by a blank line. The complete
successful stream is in [`plan-stream.success.sse`](fixtures/plan-stream.success.sse).

For normal module events the JSON object is `{ "stage": string,
"content": string }`. The POI event uses an object as `content`, the trace
event uses `{ "stage": "trace", "trace_id": string }`, and the terminal
sentinel is the literal `[DONE]` (not JSON). The Python implementation emits
the first three module events only after all Phase 1 agents finish; keep that
observable order even if Go scheduling differs.

The optional POI event is emitted only when itinerary POIs were parsed and the
map provider is available. A plan without POIs may omit it, but it must still
send `itinerary`, `budget`, `trace`, and `[DONE]` in that order.

## `POST /api/plan/regenerate`

The body extends the shared plan request with `module`, `feedback`, and
`context`. The valid itinerary example is
[`regenerate-itinerary-request.valid.json`](fixtures/regenerate-itinerary-request.valid.json).
`module` must be one of `weather`, `destination`, `accommodation`,
`itinerary`, or `budget`.

Successful response is another `text/event-stream`. For a non-itinerary module
it contains `<module>`, then `[DONE]`. For itinerary it contains
`itinerary`, optional `itinerary_pois`, then `[DONE]`; no `trace` event is
currently emitted. See [`regenerate-itinerary-stream.success.sse`](fixtures/regenerate-itinerary-stream.success.sse).

## Errors and cancellation

The invalid request fixtures demonstrate the inputs the Go service must reject:

- missing required field: [`invalid-plan-request.missing-destination.json`](fixtures/invalid-plan-request.missing-destination.json)
- invalid enum/range and reversed dates: [`invalid-plan-request.invalid-values.json`](fixtures/invalid-plan-request.invalid-values.json)
- unsupported regeneration module: [`invalid-regenerate-request.bad-module.json`](fixtures/invalid-regenerate-request.bad-module.json)

Python rejects enum/range errors through framework-generated HTTP `422` bodies,
but it currently permits reversed dates until later business logic. Its exact
validation JSON varies with the unpinned FastAPI/Pydantic version, so
[`error-validation.legacy.response.json`](fixtures/error-validation.legacy.response.json)
is a representative migration snapshot, not a Go golden response. An
unsupported module currently produces the plain HTTP `400` detail body in
[`error-regenerate-module.legacy.response.json`](fixtures/error-regenerate-module.legacy.response.json).

New Go endpoints should return a stable 4xx body with an application error code
and field map; exact provider exception text is never part of the contract. The
current stream envelope is shown in
[`error-stream.legacy.sse`](fixtures/error-stream.legacy.sse), while the
sanitized Go target is represented by
[`error-stream.sanitized.sse`](fixtures/error-stream.sanitized.sse): an `error`
event may be followed by `[DONE]`, and no later success/trace event may be
emitted.

HTTP clients may cancel either stream by closing the connection. The Go
handler must propagate request-context cancellation to planner/provider calls;
the client should treat a missing `[DONE]` after cancellation as cancelled, not
as success.

## Other frozen routes

The following routes remain available during migration:

| Method | Path | Success shape |
| --- | --- | --- |
| `GET` | `/health` | `{ "status": "ok" }` |
| `GET` | `/api/traces?limit=20` | `{ "traces": [...] }` |
| `GET` | `/api/traces/{trace_id}` | `{ "trace": {...}, "spans": [...] }` |
| `GET` | `/api/images?query=...` | `{ "images": [], "scenic_pool": [], "location": string|null }` |

The fixture set focuses on plan and regeneration bodies; these auxiliary route
shapes should receive endpoint-level tests when their Go adapters are added.
