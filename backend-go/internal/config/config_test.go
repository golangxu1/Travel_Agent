package config

import (
	"testing"

	"travel-agent/backend-go/internal/provider/llm"
)

func TestValidateRealModeRequiresProviderSettings(t *testing.T) {
	cfg := Config{Mode: "real", ModelProvider: "deepseek", ModelBaseURL: "https://api.deepseek.com/v1"}
	if err := cfg.ValidateRealMode(); llm.KindOf(err) != llm.ErrorConfiguration {
		t.Fatalf("error kind = %q, err = %v", llm.KindOf(err), err)
	}
}

func TestValidateRealModeAllowsFakeWithoutKeys(t *testing.T) {
	if err := (Config{Mode: "fake"}).ValidateRealMode(); err != nil {
		t.Fatalf("fake mode validation = %v", err)
	}
}
