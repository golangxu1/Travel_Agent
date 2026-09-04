# Fixture Index

All files in this directory are deterministic contract-test inputs or outputs.
Use the JSON files as request/response bodies and the `.sse` files as complete
wire-format response bodies. Do not compare generated Markdown prose verbatim
in production tests; assert the surrounding protocol and required fields.

| File | Use |
| --- | --- |
| `plan-request.valid.json` | Valid shared body for `/api/plan` and `/api/plan/stream`; exercises all fields and Chinese enum values. |
| `plan-response.valid.json` | Successful non-streaming response with all five module fields and a safe POI/map payload. |
| `plan-stream.success.sse` | Full successful stream, including all stages, POI data, trace event, and `[DONE]`. |
| `regenerate-itinerary-request.valid.json` | Valid itinerary regeneration body with weather/destination context. |
| `regenerate-itinerary-stream.success.sse` | Successful itinerary regeneration stream with optional POI event and `[DONE]`; no trace event. |
| `invalid-plan-request.missing-destination.json` | Missing required field; must be rejected with 4xx. |
| `invalid-plan-request.invalid-values.json` | Bad enum, reversed dates, and out-of-range people count; must be rejected before planning. |
| `invalid-regenerate-request.bad-module.json` | Unsupported module name; must be rejected with 4xx. |
| `error-validation.legacy.response.json` | Representative FastAPI/Pydantic 422 body for migration comparison only. |
| `error-regenerate-module.legacy.response.json` | Current plain FastAPI 400 body for an unsupported module, retained only for comparison. |
| `error-stream.legacy.sse` | Current stream error envelope with safe placeholder text; it documents shape, not unsafe error disclosure. |
| `error-stream.sanitized.sse` | Go target upstream failure stream; no internal exception or secret leakage. |

Fixture URLs intentionally use `/api/media/...` or `https://images.example.test`
placeholders. A real AMap key must never be added to a fixture.
