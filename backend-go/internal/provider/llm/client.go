package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Request and Response are deliberately provider-neutral. The planner should
// not know whether a request is sent to OpenAI-compatible or Anthropic APIs.
type Request struct {
	Model           string
	System          string
	User            string
	MaxOutputTokens int
}

type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

type ErrorKind string

const (
	ErrorConfiguration  ErrorKind = "configuration"
	ErrorAuthentication ErrorKind = "authentication"
	ErrorQuota          ErrorKind = "quota_exceeded"
	ErrorTimeout        ErrorKind = "timeout"
	ErrorUnavailable    ErrorKind = "upstream_unavailable"
	ErrorProtocol       ErrorKind = "invalid_response"
)

// ProviderError keeps failure classification stable without exposing provider
// response bodies or credentials to the HTTP layer and trace store.
type ProviderError struct {
	Kind       ErrorKind
	StatusCode int
}

func (e *ProviderError) Error() string {
	return "llm provider error: " + string(e.Kind)
}

func KindOf(err error) ErrorKind {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Kind
	}
	return ""
}

func ValidateConfig(provider, model, apiKey, baseURL string) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "openai" && provider != "deepseek" && provider != "anthropic" {
		return &ProviderError{Kind: ErrorConfiguration}
	}
	if strings.TrimSpace(model) == "" || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(baseURL) == "" {
		return &ProviderError{Kind: ErrorConfiguration}
	}
	return nil
}

type Client interface {
	Complete(context.Context, Request) (Response, error)
}

type HTTPClient struct {
	Provider string
	BaseURL  string
	APIKey   string
	Client   *http.Client
}

func NewHTTPClient(provider, baseURL, apiKey string, timeout time.Duration) *HTTPClient {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &HTTPClient{
		Provider: strings.ToLower(strings.TrimSpace(provider)),
		BaseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: timeout},
	}
}

func (c *HTTPClient) Complete(ctx context.Context, req Request) (Response, error) {
	if c == nil || c.Client == nil {
		return Response{}, &ProviderError{Kind: ErrorConfiguration}
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return Response{}, &ProviderError{Kind: ErrorConfiguration}
	}

	var endpoint string
	var body any
	switch c.Provider {
	case "anthropic":
		endpoint = c.BaseURL + "/v1/messages"
		body = anthropicRequest{
			Model:     req.Model,
			System:    req.System,
			MaxTokens: req.MaxOutputTokens,
			Messages:  []anthropicMessage{{Role: "user", Content: req.User}},
		}
	default:
		endpoint = c.BaseURL + "/chat/completions"
		messages := []openAIMessage{{Role: "system", Content: req.System}, {Role: "user", Content: req.User}}
		body = openAIRequest{Model: req.Model, Messages: messages, MaxTokens: req.MaxOutputTokens, Temperature: 0.2}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("marshal llm request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Response{}, &ProviderError{Kind: ErrorConfiguration}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Provider == "anthropic" {
		httpReq.Header.Set("x-api-key", c.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.Client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || (ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
			return Response{}, &ProviderError{Kind: ErrorTimeout}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return Response{}, &ProviderError{Kind: ErrorTimeout}
		}
		return Response{}, &ProviderError{Kind: ErrorUnavailable}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return Response{}, fmt.Errorf("read llm response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := ErrorUnavailable
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			kind = ErrorAuthentication
		case http.StatusPaymentRequired, http.StatusTooManyRequests:
			kind = ErrorQuota
		case http.StatusBadRequest:
			kind = ErrorProtocol
		}
		return Response{}, &ProviderError{Kind: kind, StatusCode: resp.StatusCode}
	}

	if c.Provider == "anthropic" {
		var result anthropicResponse
		if err := json.Unmarshal(data, &result); err != nil {
			return Response{}, &ProviderError{Kind: ErrorProtocol}
		}
		return Response{Text: firstAnthropicText(result.Content), InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens}, nil
	}

	var result openAIResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return Response{}, &ProviderError{Kind: ErrorProtocol}
	}
	if len(result.Choices) == 0 {
		return Response{}, &ProviderError{Kind: ErrorProtocol}
	}
	return Response{
		Text:         result.Choices[0].Message.Content,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
	}, nil
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func firstAnthropicText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	for _, item := range content {
		if item.Type == "text" {
			return item.Text
		}
	}
	return ""
}
