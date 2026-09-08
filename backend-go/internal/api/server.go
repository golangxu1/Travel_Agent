package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"travel-agent/backend-go/internal/domain"
	"travel-agent/backend-go/internal/planner"
	"travel-agent/backend-go/internal/provider/llm"
	"travel-agent/backend-go/internal/trace"
)

const (
	defaultMaxBodyBytes       int64 = 64 * 1024
	plannerUnavailableCode          = "planner_unavailable"
	plannerUnavailableMessage       = "计划生成暂时失败，请稍后重试。"
)

// Server owns HTTP handlers and the planner boundary. It intentionally uses
// net/http so the first migration stage has no framework or dependency lock-in.
type Server struct {
	Planner        planner.Planner
	Media          MediaService
	Traces         trace.Repository
	MaxBodyBytes   int64
	AllowedOrigins []string
}

// MediaService covers the optional AMap and media-proxy boundary. Keeping it
// behind this small interface lets API tests avoid network access entirely.
type MediaService interface {
	SearchImages(context.Context, string, int, int) (domain.ImageSearchResult, error)
	ProxyPhoto(context.Context, string) (domain.MediaResponse, error)
	ProxyStaticMap(context.Context, string) (domain.MediaResponse, error)
}

func NewServer(p planner.Planner, dependencies ...any) *Server {
	if p == nil {
		p = planner.NewFakePlanner()
	}
	server := &Server{Planner: p, MaxBodyBytes: defaultMaxBodyBytes}
	for _, dependency := range dependencies {
		switch value := dependency.(type) {
		case MediaService:
			server.Media = value
		case trace.Repository:
			server.Traces = value
		}
	}
	return server
}

// Routes returns the compatibility routes implemented by the phase-0 Go
// service. Trace storage is injected separately so it can later be swapped
// for PostgreSQL without changing the HTTP contract.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/api/plan", s.plan)
	mux.HandleFunc("/api/plan/stream", s.planStream)
	mux.HandleFunc("/api/plan/regenerate", s.planRegenerate)
	mux.HandleFunc("/api/images", s.images)
	mux.HandleFunc("/api/poi-photo", s.poiPhoto)
	mux.HandleFunc("/api/maps/static", s.staticMap)
	mux.HandleFunc("/api/traces", s.traces)
	mux.HandleFunc("/api/traces/", s.traceDetail)
	return s.withCORS(mux)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		allowed := origin != "" && s.originAllowed(origin)
		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			if origin != "" && !allowed {
				writeJSON(w, http.StatusForbidden, requestError{Success: false, Code: "origin_not_allowed", Error: "请求来源不允许"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), origin) {
			return true
		}
	}
	return false
}

func (s *Server) traces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	limit, err := boundedQueryInt(r, "limit", 20, 1, 100)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	if s.Traces == nil {
		writeJSON(w, http.StatusOK, map[string]any{"traces": []trace.Trace{}})
		return
	}
	items, err := s.Traces.List(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, requestError{Success: false, Code: "trace_unavailable", Error: "追踪数据暂时不可用，请稍后重试。"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": items})
}

func (s *Server) traceDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/traces/")
	if id == "" || strings.Contains(id, "/") || len(id) > 128 {
		writeJSON(w, http.StatusBadRequest, requestError{Success: false, Code: "invalid_trace_id", Error: "追踪 ID 无效"})
		return
	}
	if s.Traces == nil {
		writeJSON(w, http.StatusNotFound, requestError{Success: false, Code: "trace_not_found", Error: "追踪记录不存在"})
		return
	}
	item, err := s.Traces.Get(r.Context(), id)
	if errors.Is(err, trace.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, requestError{Success: false, Code: "trace_not_found", Error: "追踪记录不存在"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, requestError{Success: false, Code: "trace_unavailable", Error: "追踪数据暂时不可用，请稍后重试。"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" || len([]rune(query)) > 100 {
		writeRequestError(w, &domain.ValidationError{Field: "query", Message: "必须是 1 到 100 个字符"})
		return
	}
	count, err := boundedQueryInt(r, "count", 4, 1, 4)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	poolLimit, err := boundedQueryInt(r, "pool_limit", 24, 1, 24)
	if err != nil {
		writeRequestError(w, err)
		return
	}
	if s.Media == nil {
		writeJSON(w, http.StatusOK, domain.ImageSearchResult{Images: []domain.ImageEntry{}, ScenicPool: []domain.ImageEntry{}, Location: nil})
		return
	}
	result, err := s.Media.SearchImages(r.Context(), query, count, poolLimit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, requestError{Success: false, Code: "media_unavailable", Error: "地图服务暂时不可用，请稍后重试。"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) poiPhoto(w http.ResponseWriter, r *http.Request) {
	s.proxyMedia(w, r, "photo")
}

func (s *Server) staticMap(w http.ResponseWriter, r *http.Request) {
	s.proxyMedia(w, r, "static-map")
}

func (s *Server) proxyMedia(w http.ResponseWriter, r *http.Request, kind string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" || len(token) > 256 || s.Media == nil {
		mediaNotFound(w)
		return
	}
	var (
		media domain.MediaResponse
		err   error
	)
	if kind == "photo" {
		media, err = s.Media.ProxyPhoto(r.Context(), token)
	} else {
		media, err = s.Media.ProxyStaticMap(r.Context(), token)
	}
	if err != nil || !strings.HasPrefix(strings.ToLower(media.ContentType), "image/") || len(media.Body) == 0 {
		mediaNotFound(w)
		return
	}
	w.Header().Set("Content-Type", media.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(media.Body)
}

func mediaNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, requestError{Success: false, Code: "media_not_found", Error: "图片资源不可用"})
}

func boundedQueryInt(r *http.Request, name string, fallback, min, max int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, &domain.ValidationError{Field: name, Message: fmt.Sprintf("必须在 %d 到 %d 之间", min, max)}
	}
	return value, nil
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var payload planRequestPayload
	if err := decodeJSON(w, r, &payload, s.bodyLimit()); err != nil {
		writeRequestError(w, err)
		return
	}
	req := payload.domainRequest()
	if err := domain.ValidatePlan(req); err != nil {
		writeRequestError(w, err)
		return
	}
	defer r.Body.Close()

	response, err := s.Planner.Plan(r.Context(), req)
	if err != nil {
		if isCanceled(r.Context(), err) {
			return
		}
		writePlannerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) planStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var payload planRequestPayload
	if err := decodeJSON(w, r, &payload, s.bodyLimit()); err != nil {
		writeRequestError(w, err)
		return
	}
	req := payload.domainRequest()
	if err := domain.ValidatePlan(req); err != nil {
		writeRequestError(w, err)
		return
	}
	defer r.Body.Close()

	stream, err := s.Planner.Stream(r.Context(), req)
	if err != nil {
		if isCanceled(r.Context(), err) {
			return
		}
		writePlannerError(w, err)
		return
	}
	if stream == nil || stream.Events == nil {
		writePlannerError(w, errors.New("planner returned an empty stream"))
		return
	}

	writer := newSSEWriter(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream.Events:
			if !ok {
				if r.Context().Err() != nil {
					return
				}
				if err := receiveStreamError(stream); err != nil && !isCanceled(r.Context(), err) {
					_ = writer.Write(plannerErrorEvent(err))
				}
				_ = writer.Done()
				return
			}
			if err := writer.Write(event); err != nil {
				return
			}
		}
	}
}

func (s *Server) planRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var payload regenerateRequestPayload
	if err := decodeJSON(w, r, &payload, s.bodyLimit()); err != nil {
		writeRequestError(w, err)
		return
	}
	if payload.Context.set && payload.Context.null {
		writeRequestError(w, &domain.ValidationError{Field: "context", Message: "必须是对象"})
		return
	}
	req := payload.domainRequest()
	if err := domain.ValidateRegenerate(req); err != nil {
		writeRequestError(w, err)
		return
	}
	defer r.Body.Close()

	stream, err := s.Planner.RegenerateStream(r.Context(), req)
	if err != nil {
		if isCanceled(r.Context(), err) {
			return
		}
		writePlannerError(w, err)
		return
	}
	if stream == nil || stream.Events == nil {
		writePlannerError(w, errors.New("planner returned an empty stream"))
		return
	}

	writer := newSSEWriter(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream.Events:
			if !ok {
				if r.Context().Err() != nil {
					return
				}
				if err := receiveStreamError(stream); err != nil && !isCanceled(r.Context(), err) {
					_ = writer.Write(plannerErrorEvent(err))
				}
				_ = writer.Done()
				return
			}
			if err := writer.Write(event); err != nil {
				return
			}
		}
	}
}

func (s *Server) bodyLimit() int64 {
	if s.MaxBodyBytes <= 0 {
		return defaultMaxBodyBytes
	}
	return s.MaxBodyBytes
}

// planRequestPayload tracks optional fields so omitted values receive the same
// defaults as schemas.py, while explicit zero/empty values still fail validation.
type planRequestPayload struct {
	Origin      string          `json:"origin"`
	Destination string          `json:"destination"`
	StartDate   string          `json:"start_date"`
	EndDate     string          `json:"end_date"`
	Transport   optionalString  `json:"transport"`
	Preferences optionalStrings `json:"preferences"`
	People      optionalInt     `json:"people"`
	BudgetLevel *string         `json:"budget_level"`
}

func (p planRequestPayload) domainRequest() domain.PlanRequest {
	req := domain.PlanRequest{
		Origin:      p.Origin,
		Destination: p.Destination,
		StartDate:   p.StartDate,
		EndDate:     p.EndDate,
		Transport:   domain.TransportMixed,
		Preferences: []string{domain.TravelStyleScenery},
		People:      2,
		BudgetLevel: p.BudgetLevel,
	}
	if p.Transport.set {
		req.Transport = p.Transport.value
	}
	if p.Preferences.set {
		req.Preferences = p.Preferences.value
	}
	if p.People.set {
		req.People = p.People.value
	}
	return req
}

type regenerateRequestPayload struct {
	planRequestPayload
	Module   string      `json:"module"`
	Feedback string      `json:"feedback"`
	Context  optionalMap `json:"context"`
}

func (p regenerateRequestPayload) domainRequest() domain.RegenerateRequest {
	return domain.RegenerateRequest{
		PlanRequest: p.planRequestPayload.domainRequest(),
		Module:      p.Module,
		Feedback:    p.Feedback,
		Context:     p.Context.value,
	}
}

// The following optional wrappers retain JSON-field presence. The legacy
// schema defaults omitted transport/preferences/people, but explicit null is
// invalid and must not silently turn into the same default.
type optionalString struct {
	set   bool
	value string
}

func (v *optionalString) UnmarshalJSON(data []byte) error {
	v.set = true
	if isJSONNull(data) {
		v.value = ""
		return nil
	}
	return json.Unmarshal(data, &v.value)
}

type optionalStrings struct {
	set   bool
	value []string
}

func (v *optionalStrings) UnmarshalJSON(data []byte) error {
	v.set = true
	if isJSONNull(data) {
		v.value = nil
		return nil
	}
	return json.Unmarshal(data, &v.value)
}

type optionalInt struct {
	set   bool
	value int
}

func (v *optionalInt) UnmarshalJSON(data []byte) error {
	v.set = true
	if isJSONNull(data) {
		v.value = 0
		return nil
	}
	return json.Unmarshal(data, &v.value)
}

type optionalMap struct {
	set   bool
	null  bool
	value map[string]any
}

func (v *optionalMap) UnmarshalJSON(data []byte) error {
	v.set = true
	v.null = isJSONNull(data)
	if v.null {
		v.value = nil
		return nil
	}
	return json.Unmarshal(data, &v.value)
}

func isJSONNull(data []byte) bool {
	return bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any, limit int64) error {
	if r.Body == nil {
		return errors.New("请求体不能为空")
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("请求体不能为空")
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return errors.New("请求体过大")
		}
		return errors.New("请求体不是有效 JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("请求体只能包含一个 JSON 对象")
		}
		return errors.New("请求体不是有效 JSON")
	}
	return nil
}

type requestError struct {
	Success bool              `json:"success"`
	Code    string            `json:"code"`
	Error   string            `json:"error"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeRequestError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "invalid_request"
	var fields map[string]string
	if strings.Contains(err.Error(), "请求体过大") {
		status = http.StatusRequestEntityTooLarge
		code = "request_too_large"
	}
	var validationErr *domain.ValidationError
	if errors.As(err, &validationErr) {
		fields = map[string]string{validationErr.Field: validationErr.Message}
	}
	writeJSON(w, status, requestError{Success: false, Code: code, Error: err.Error(), Fields: fields})
}

func writePlannerError(w http.ResponseWriter, err error) {
	code, message, status := plannerErrorDetails(err)
	writeJSON(w, status, requestError{
		Success: false,
		Code:    code,
		Error:   message,
	})
}

func plannerErrorEvent(err error) domain.StageEvent {
	code, message, _ := plannerErrorDetails(err)
	return domain.StageEvent{Error: message, Code: code}
}

func plannerErrorDetails(err error) (string, string, int) {
	switch llm.KindOf(err) {
	case llm.ErrorConfiguration:
		return "llm_configuration_error", "大模型配置不完整或不受支持，请检查服务端配置。", http.StatusInternalServerError
	case llm.ErrorAuthentication:
		return "llm_authentication_failed", "大模型认证失败，请检查 API Key。", http.StatusBadGateway
	case llm.ErrorQuota:
		return "llm_quota_exceeded", "大模型额度或调用频率受限，请检查账户状态。", http.StatusBadGateway
	case llm.ErrorTimeout:
		return "llm_timeout", "大模型请求超时，请稍后重试。", http.StatusGatewayTimeout
	case llm.ErrorUnavailable:
		return "llm_upstream_unavailable", "大模型服务暂时不可用，请稍后重试。", http.StatusBadGateway
	case llm.ErrorProtocol:
		return "llm_invalid_response", "大模型返回格式异常，请稍后重试。", http.StatusBadGateway
	default:
		return plannerUnavailableCode, plannerUnavailableMessage, http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeJSON(w, http.StatusMethodNotAllowed, requestError{Success: false, Code: "method_not_allowed", Error: "请求方法不支持"})
}

func isCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil
}

// sseWriter centralizes the exact framing required by the current frontend.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)
	return &sseWriter{w: w, flusher: flusher}
}

func (s *sseWriter) Write(event domain.StageEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func (s *sseWriter) Done() error {
	if _, err := io.WriteString(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

func receiveStreamError(stream *planner.EventStream) error {
	if stream == nil || stream.Err == nil {
		return nil
	}
	var first error
	for err := range stream.Err {
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}
