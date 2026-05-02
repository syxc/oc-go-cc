// Package router defines HTTP route registration and middleware chaining,
// as well as model selection based on request scenarios.
package router

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"oc-go-cc/internal/config"
)

const scenarioLongContext = "long_context"

// ModelRouter handles model selection based on scenarios.
type ModelRouter struct {
	config *config.Config
}

// NewModelRouter creates a new model router.
func NewModelRouter(cfg *config.Config) *ModelRouter {
	return &ModelRouter{config: cfg}
}

// RouteResult contains the selected model and fallback chain.
type RouteResult struct {
	Primary   config.ModelConfig
	Fallbacks []config.ModelConfig
	Scenario  Scenario
}

// FindModelByID looks up a model config by its model_id across all scenarios.
// It also recognizes Claude Code aliases and model names with context suffixes
// such as deepseek-v4-pro[1m].
func (r *ModelRouter) FindModelByID(modelID string) (config.ModelConfig, []config.ModelConfig, bool) {
	if modelID == "" {
		return config.ModelConfig{}, nil, false
	}

	// 1. Claude Code env-var mapping is configuration-driven. When the requested
	// model matches one of Claude Code's configured aliases, reuse the scenario's
	// tuning/fallbacks but swap in the configured target model name.
	if mc, fallbacks, ok := r.resolveClaudeCodeEnvModel(modelID); ok {
		return mc, fallbacks, true
	}

	// 2. Direct model_id match across configured models, including custom keys.
	priority := []string{"default", "complex", "think", "long_context", "background", "fast"}
	for _, scenario := range priority {
		if mc, ok := r.config.Models[scenario]; ok && matchesRequestedModelID(mc.ModelID, modelID) {
			return mc, r.config.Fallbacks[scenario], true
		}
	}

	var extraKeys []string
	for key := range r.config.Models {
		if contains(priority, key) {
			continue
		}
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		if mc := r.config.Models[key]; matchesRequestedModelID(mc.ModelID, modelID) {
			return mc, r.config.Fallbacks[key], true
		}
	}

	// 3. Legacy Claude variant-name fallback.
	variantScenario := r.resolveClaudeCodeScenario(modelID)
	if variantScenario == "" {
		return config.ModelConfig{}, nil, false
	}
	if mc, ok := r.config.Models[variantScenario]; ok {
		return mc, r.config.Fallbacks[variantScenario], true
	}

	return config.ModelConfig{}, nil, false
}

func (r *ModelRouter) resolveClaudeCodeEnvModel(modelID string) (config.ModelConfig, []config.ModelConfig, bool) {
	requestedBase, requestedContext := normalizeRequestedModelID(modelID)
	if requestedBase == "" {
		return config.ModelConfig{}, nil, false
	}

	for _, mapping := range claudeCodeEnvMappings() {
		alias := os.Getenv(mapping.EnvName)
		if alias == "" || !matchesRequestedModelID(alias, modelID) {
			continue
		}

		scenario := mapping.Scenario
		if requestedContext == scenarioLongContext {
			scenario = scenarioLongContext
		}

		template, ok := r.config.Models[scenario]
		if !ok {
			return config.ModelConfig{}, nil, false
		}

		resolved := template
		resolved.ModelID = requestedBase
		return resolved, r.config.Fallbacks[scenario], true
	}

	return config.ModelConfig{}, nil, false
}

func (r *ModelRouter) resolveClaudeCodeScenario(modelID string) string {
	requestedBase, _ := normalizeRequestedModelID(modelID)

	for _, mapping := range claudeCodeEnvMappings() {
		alias := os.Getenv(mapping.EnvName)
		if alias == "" || !matchesRequestedModelID(alias, modelID) {
			continue
		}
		return mapping.Scenario
	}

	lowerBase := strings.ToLower(requestedBase)
	switch {
	case strings.Contains(lowerBase, "haiku"):
		return "background"
	case strings.Contains(lowerBase, "opus"):
		return "complex"
	case strings.Contains(lowerBase, "sonnet"):
		return "default"
	default:
		return ""
	}
}

func claudeCodeEnvMappings() []struct {
	EnvName  string
	Scenario string
} {
	return []struct {
		EnvName  string
		Scenario string
	}{
		{EnvName: "ANTHROPIC_MODEL", Scenario: "default"},
		{EnvName: "ANTHROPIC_DEFAULT_HAIKU_MODEL", Scenario: "background"},
		{EnvName: "ANTHROPIC_DEFAULT_SONNET_MODEL", Scenario: "default"},
		{EnvName: "ANTHROPIC_DEFAULT_OPUS_MODEL", Scenario: "complex"},
		{EnvName: "CLAUDE_CODE_SUBAGENT_MODEL", Scenario: "background"},
	}
}

func matchesRequestedModelID(configuredModelID, requestedModelID string) bool {
	configuredBase, configuredContext := normalizeRequestedModelID(configuredModelID)
	requestedBase, requestedContext := normalizeRequestedModelID(requestedModelID)

	if configuredBase == "" || requestedBase == "" {
		return false
	}
	if !strings.EqualFold(configuredBase, requestedBase) {
		return false
	}
	return configuredContext == "" || requestedContext == "" || configuredContext == requestedContext
}

func normalizeRequestedModelID(modelID string) (string, string) {
	trimmed := strings.TrimSpace(modelID)
	if trimmed == "" {
		return "", ""
	}

	open := strings.LastIndex(trimmed, "[")
	if open == -1 || !strings.HasSuffix(trimmed, "]") {
		return trimmed, ""
	}

	base := strings.TrimSpace(trimmed[:open])
	if base == "" {
		return trimmed, ""
	}

	suffix := strings.ToLower(strings.TrimSpace(trimmed[open+1 : len(trimmed)-1]))
	switch suffix {
	case "1m", "long-context", "long_context":
		return base, scenarioLongContext
	default:
		return base, ""
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// Route determines which model to use for a request.
func (r *ModelRouter) Route(messages []MessageContent, tokenCount int) (RouteResult, error) {
	result := DetectScenario(messages, tokenCount, r.config)

	// Get primary model for scenario
	primary, ok := r.config.Models[string(result.Scenario)]
	if !ok {
		// Fall back to default if scenario model not configured
		primary, ok = r.config.Models["default"]
		if !ok {
			return RouteResult{}, fmt.Errorf("no default model configured")
		}
	}

	// Get fallbacks for scenario
	fallbacks := r.config.Fallbacks[string(result.Scenario)]
	if len(fallbacks) == 0 {
		// Fall back to default fallbacks
		fallbacks = r.config.Fallbacks["default"]
	}

	return RouteResult{
		Primary:   primary,
		Fallbacks: fallbacks,
		Scenario:  result.Scenario,
	}, nil
}

// GetModelChain returns the full chain of models to try (primary + fallbacks).
func (rr *RouteResult) GetModelChain() []config.ModelConfig {
	chain := []config.ModelConfig{rr.Primary}
	chain = append(chain, rr.Fallbacks...)
	return chain
}

// RouteForStreaming determines which model to use for streaming requests.
// Prioritizes fast TTFT (time-to-first-token) over capability.
func (r *ModelRouter) RouteForStreaming(messages []MessageContent, tokenCount int) RouteResult {
	result := RouteForStreaming(messages, tokenCount, r.config)

	// Get primary model for scenario
	primary, ok := r.config.Models[string(result.Scenario)]
	if !ok {
		// Fall back to fast scenario if not configured
		primary, ok = r.config.Models["fast"]
		if !ok {
			// Fall back to default
			primary = r.config.Models["default"]
		}
	}

	// Get fallbacks for scenario
	fallbacks := r.config.Fallbacks[string(result.Scenario)]
	if len(fallbacks) == 0 {
		// Fall back to fast fallbacks
		fallbacks = r.config.Fallbacks["fast"]
	}

	return RouteResult{
		Primary:   primary,
		Fallbacks: fallbacks,
		Scenario:  result.Scenario,
	}
}
