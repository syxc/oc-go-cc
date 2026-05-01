// Package router defines HTTP route registration and middleware chaining,
// as well as model selection based on request scenarios.
package router

import (
	"fmt"
	"strings"

	"oc-go-cc/internal/config"
)

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
// Also recognizes Claude Code model variants (haiku/sonnet/opus) and maps them
// to configured scenarios: haiku→background, sonnet→default, opus→complex.
func (r *ModelRouter) FindModelByID(modelID string) (config.ModelConfig, []config.ModelConfig, bool) {
	if modelID == "" {
		return config.ModelConfig{}, nil, false
	}

	// 1. Direct model_id match across configured scenarios (priority order).
	priority := []string{"default", "complex", "think", "long_context", "background", "fast"}
	for _, scenario := range priority {
		if mc, ok := r.config.Models[scenario]; ok && mc.ModelID == modelID {
			return mc, r.config.Fallbacks[scenario], true
		}
	}

	// 2. Claude Code variant mapping — haiku→cheap, sonnet→balanced, opus→capable.
	lower := strings.ToLower(modelID)
	var variantScenario string
	switch {
	case strings.Contains(lower, "haiku"):
		variantScenario = "background"
	case strings.Contains(lower, "opus"):
		variantScenario = "complex"
	case strings.Contains(lower, "sonnet"):
		variantScenario = "default"
	default:
		return config.ModelConfig{}, nil, false
	}
	if mc, ok := r.config.Models[variantScenario]; ok {
		return mc, r.config.Fallbacks[variantScenario], true
	}

	return config.ModelConfig{}, nil, false
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
