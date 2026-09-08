package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"travel-agent/backend-go/internal/provider/llm"
)

const defaultAddress = ":8001"

// Config contains only the phase-0 process settings. Provider credentials are
// intentionally absent until real adapters and their secret-handling policy
// are introduced in a later migration stage.
type Config struct {
	Address         string
	Mode            string
	ModelProvider   string
	ModelID         string
	ModelAPIKey     string
	ModelBaseURL    string
	ModelTimeout    time.Duration
	FakeStageDelay  time.Duration
	MaxOutputTokens int
	AMapAPIKey      string
	PublicBaseURL   string
	TraceDBPath     string
	AllowedOrigins  []string
}

func (c Config) ValidateRealMode() error {
	if c.Mode != "real" {
		return nil
	}
	return llm.ValidateConfig(c.ModelProvider, c.ModelID, c.ModelAPIKey, c.ModelBaseURL)
}

// Load reads optional local settings without mutating process environment.
func Load() Config {
	address := os.Getenv("TRAVEL_AGENT_GO_ADDR")
	if address == "" {
		address = defaultAddress
	}

	provider := os.Getenv("MODEL_PROVIDER")
	if provider == "" {
		provider = "openai"
	}
	modelID := os.Getenv("MODEL_ID")
	apiKey := os.Getenv("MODEL_API_KEY")
	mode := os.Getenv("TRAVEL_AGENT_MODE")
	if mode == "" {
		mode = "real"
		if apiKey == "" || modelID == "" {
			mode = "fake"
		}
	}

	baseURL := os.Getenv("MODEL_BASE_URL")
	if baseURL == "" {
		switch provider {
		case "deepseek":
			baseURL = "https://api.deepseek.com/v1"
		case "anthropic":
			baseURL = "https://api.anthropic.com"
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}

	modelTimeout := 120 * time.Second
	if raw := os.Getenv("MODEL_TIMEOUT_SECONDS"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			modelTimeout = time.Duration(seconds) * time.Second
		}
	}

	stageDelay := time.Duration(0)
	if raw := os.Getenv("TRAVEL_AGENT_FAKE_STAGE_DELAY_MS"); raw != "" {
		if milliseconds, err := strconv.Atoi(raw); err == nil && milliseconds >= 0 {
			stageDelay = time.Duration(milliseconds) * time.Millisecond
		}
	}

	maxOutputTokens := 4096
	if raw := os.Getenv("MODEL_MAX_OUTPUT_TOKENS"); raw != "" {
		if tokens, err := strconv.Atoi(raw); err == nil && tokens > 0 {
			maxOutputTokens = tokens
		}
	}

	publicBaseURL := os.Getenv("TRAVEL_AGENT_PUBLIC_BASE_URL")
	if publicBaseURL == "" {
		publicBaseURL = "http://localhost:8001"
	}
	traceDBPath := os.Getenv("TRAVEL_AGENT_DB_PATH")
	if traceDBPath == "" {
		traceDBPath = filepath.Join("data", "travel-agent.db")
	}
	allowedOrigins := []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	if raw := os.Getenv("TRAVEL_AGENT_CORS_ORIGINS"); raw != "" {
		allowedOrigins = allowedOrigins[:0]
		for _, origin := range strings.Split(raw, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				allowedOrigins = append(allowedOrigins, strings.TrimRight(origin, "/"))
			}
		}
	}

	return Config{
		Address:         address,
		Mode:            mode,
		ModelProvider:   provider,
		ModelID:         modelID,
		ModelAPIKey:     apiKey,
		ModelBaseURL:    baseURL,
		ModelTimeout:    modelTimeout,
		FakeStageDelay:  stageDelay,
		MaxOutputTokens: maxOutputTokens,
		AMapAPIKey:      os.Getenv("AMAP_API_KEY"),
		PublicBaseURL:   publicBaseURL,
		TraceDBPath:     traceDBPath,
		AllowedOrigins:  allowedOrigins,
	}
}
