// Package config handles application configuration loading and validation.
package config

import "encoding/json"

// Config holds the complete application configuration.
type Config struct {
	APIKey                         string                   `json:"api_key"`
	Host                           string                   `json:"host"`
	Port                           int                      `json:"port"`
	EnableStreamingScenarioRouting bool                     `json:"enable_streaming_scenario_routing"`
	DefaultResponseModel           string                   `json:"default_response_model"`
	Models                         map[string]ModelConfig   `json:"models"`
	Fallbacks                      map[string][]ModelConfig `json:"fallbacks"`
	OpenCodeGo                     OpenCodeGoConfig         `json:"opencode_go"`
	Logging                        LoggingConfig            `json:"logging"`
}

// ModelConfig defines routing rules for a specific model.
type ModelConfig struct {
	Provider         string          `json:"provider"`
	ModelID          string          `json:"model_id"`
	Endpoint         string          `json:"endpoint,omitempty"`
	Temperature      float64         `json:"temperature"`
	MaxTokens        int             `json:"max_tokens"`
	ContextThreshold int             `json:"context_threshold"`
	ReasoningEffort  string          `json:"reasoning_effort"`
	Thinking         json.RawMessage `json:"thinking,omitempty"`
}

// ShouldForwardRaw returns true if requests for this model should bypass
// Anthropic-to-OpenAI transformation and be forwarded directly to the
// Anthropic endpoint.  When Endpoint is empty or "auto", the legacy
// IsAnthropicModel() heuristic is used as a fallback for backward
// compatibility.  Set Endpoint to "anthropic" for explicit raw forwarding,
// or "openai" to always transform.
func (m ModelConfig) ShouldForwardRaw() bool {
	switch m.Endpoint {
	case "anthropic":
		return true
	case "openai":
		return false
	default:
		// empty or "auto": fall back to legacy heuristic
		return IsLegacyAnthropicModel(m.ModelID)
	}
}

// IsLegacyAnthropicModel returns true if the model is known to speak the
// Anthropic API natively.  This is the legacy heuristic used before the
// Endpoint field was introduced.  Prefer setting Endpoint: "anthropic" in
// config for new models.
func IsLegacyAnthropicModel(modelID string) bool {
	switch modelID {
	case "minimax-m2.5", "minimax-m2.7":
		return true
	default:
		return false
	}
}

// OpenCodeGoConfig holds the upstream OpenCode Go API settings.
type OpenCodeGoConfig struct {
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	TimeoutMs        int    `json:"timeout_ms"`
}

// LoggingConfig controls application logging behavior.
type LoggingConfig struct {
	Level    string `json:"level"`
	Requests bool   `json:"requests"`
}
