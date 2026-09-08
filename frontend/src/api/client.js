// All browser requests go through this small compatibility client. Set
// VITE_API_BASE_URL selects a separate Go dev server. An explicitly empty
// value uses same-origin /api requests; when omitted, local Go uses :8001.
const configuredBaseURL = import.meta.env.VITE_API_BASE_URL;
const API_BASE_URL = (configuredBaseURL === undefined ? "http://localhost:8001" : configuredBaseURL).replace(/\/$/, "");

function apiURL(path) {
  return `${API_BASE_URL}${path.startsWith("/") ? path : `/${path}`}`;
}

async function assertResponse(response) {
  if (response.ok) return response;
  let message = `HTTP ${response.status}`;
  try {
    const body = await response.json();
    message = body.error || body.detail || message;
  } catch { /* keep the HTTP status */ }
  throw new Error(message);
}

export async function fetchJSON(path, options = {}) {
  const response = await fetch(apiURL(path), options);
  await assertResponse(response);
  return response.json();
}

// Consumes the server's data: JSON\n\n SSE protocol. It handles arbitrary
// network chunk boundaries, CRLF, multiline data fields, UTF-8 flushing and
// the [DONE] sentinel used by both Python and Go during the migration.
export async function streamSSE(path, options, onEvent) {
  const response = await fetch(apiURL(path), options);
  await assertResponse(response);
  if (!response.body) throw new Error("响应不支持流式读取");

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let dataLines = [];
  let finished = false;

  const dispatch = () => {
    if (dataLines.length === 0) return;
    const payload = dataLines.join("\n").trim();
    dataLines = [];
    if (!payload) return;
    if (payload === "[DONE]") {
      finished = true;
      return;
    }
    let parsed;
    try {
      parsed = JSON.parse(payload);
    } catch {
      return;
    }
    if (parsed.error) throw new Error(parsed.error);
    onEvent(parsed);
  };

  const consumeLines = (text) => {
    buffer += text;
    const lines = buffer.split(/\r?\n/);
    buffer = lines.pop() || "";
    for (const line of lines) {
      if (line === "") dispatch();
      else if (line.startsWith("data:")) dataLines.push(line.slice(5).replace(/^ /, ""));
    }
  };

  while (!finished) {
    const { done, value } = await reader.read();
    if (done) break;
    consumeLines(decoder.decode(value, { stream: true }));
  }
  if (!finished) {
    consumeLines(decoder.decode());
    if (buffer === "") dispatch();
  }
}

export function jsonRequest(body, signal) {
  return {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  };
}
