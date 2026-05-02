package router

import (
	"testing"

	"github.com/syxc/oc-go-cc/internal/config"
)

func TestFindModelByID_MatchesCustomModelKey(t *testing.T) {
	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"deepseek_v4_max": {
				ModelID: "deepseek-v4-pro",
			},
		},
	})

	model, _, ok := router.FindModelByID("deepseek-v4-pro")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "deepseek-v4-pro" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "deepseek-v4-pro")
	}
}

func TestFindModelByID_UsesNormalizedDirectMatchWithoutContextSuffix(t *testing.T) {
	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"deepseek_v4_max": {
				ModelID: "deepseek-v4-pro",
			},
		},
	})

	model, _, ok := router.FindModelByID(" deepseek-v4-pro ")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "deepseek-v4-pro" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "deepseek-v4-pro")
	}
}

func TestFindModelByID_NormalizesLongContextSuffix(t *testing.T) {
	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"deepseek_v4_max": {
				ModelID: "deepseek-v4-pro",
			},
		},
	})

	model, _, ok := router.FindModelByID("deepseek-v4-pro[1m]")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "deepseek-v4-pro" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "deepseek-v4-pro")
	}
}

func TestFindModelByID_UsesClaudeCodeEnvMappingForOpusSlot(t *testing.T) {
	t.Setenv("ANTHROPIC_DEFAULT_OPUS_MODEL", "deepseek-v4-pro[1m]")

	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"long_context": {
				ModelID:         "minimax-m2.5",
				MaxTokens:       16384,
				ReasoningEffort: "max",
			},
		},
	})

	model, _, ok := router.FindModelByID("deepseek-v4-pro[1m]")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "deepseek-v4-pro" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "deepseek-v4-pro")
	}
	if model.MaxTokens != 16384 {
		t.Fatalf("MaxTokens = %d, want %d", model.MaxTokens, 16384)
	}
	if model.ReasoningEffort != "max" {
		t.Fatalf("ReasoningEffort = %q, want %q", model.ReasoningEffort, "max")
	}
}

func TestFindModelByID_UsesClaudeCodeEnvMappingForHaiku(t *testing.T) {
	t.Setenv("ANTHROPIC_DEFAULT_HAIKU_MODEL", "deepseek-v4-flash")

	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"background": {
				ModelID:     "deepseek-v4-flash",
				Temperature: 0.3,
				MaxTokens:   2048,
			},
		},
	})

	model, _, ok := router.FindModelByID("deepseek-v4-flash")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "deepseek-v4-flash" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "deepseek-v4-flash")
	}
	if model.Temperature != 0.3 {
		t.Fatalf("Temperature = %v, want %v", model.Temperature, 0.3)
	}
	if model.MaxTokens != 2048 {
		t.Fatalf("MaxTokens = %d, want %d", model.MaxTokens, 2048)
	}
}

func TestFindModelByID_UsesClaudeCodeEnvMappingWithoutExplicitModelConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_DEFAULT_SONNET_MODEL", "qwen3.5-plus")

	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"default": {
				ModelID:     "deepseek-v4-pro",
				Temperature: 0.7,
				MaxTokens:   8192,
			},
		},
	})

	model, _, ok := router.FindModelByID("qwen3.5-plus")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "qwen3.5-plus" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "qwen3.5-plus")
	}
	if model.Temperature != 0.7 {
		t.Fatalf("Temperature = %v, want %v", model.Temperature, 0.7)
	}
	if model.MaxTokens != 8192 {
		t.Fatalf("MaxTokens = %d, want %d", model.MaxTokens, 8192)
	}
}

func TestFindModelByID_UsesClaudeCodeSubagentMappingWithoutExplicitModelConfig(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "qwen3.5-plus")

	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"background": {
				ModelID:     "deepseek-v4-flash",
				Temperature: 0.3,
				MaxTokens:   2048,
			},
		},
	})

	model, _, ok := router.FindModelByID("qwen3.5-plus")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "qwen3.5-plus" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "qwen3.5-plus")
	}
	if model.Temperature != 0.3 {
		t.Fatalf("Temperature = %v, want %v", model.Temperature, 0.3)
	}
	if model.MaxTokens != 2048 {
		t.Fatalf("MaxTokens = %d, want %d", model.MaxTokens, 2048)
	}
}
