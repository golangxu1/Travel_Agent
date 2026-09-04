package config

import (
	"os"
	"strconv"
	"time"
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
	}
}
