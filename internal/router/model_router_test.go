package router

import (
	"testing"

	"oc-go-cc/internal/config"
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
			"complex": {
				ModelID: "glm-5.1",
			},
		},
	})

	model, _, ok := router.FindModelByID("deepseek-v4-pro[1m]")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "glm-5.1" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "glm-5.1")
	}
}

func TestFindModelByID_UsesClaudeCodeEnvMappingForHaiku(t *testing.T) {
	t.Setenv("ANTHROPIC_DEFAULT_HAIKU_MODEL", "deepseek-v4-flash")

	router := NewModelRouter(&config.Config{
		Models: map[string]config.ModelConfig{
			"background": {
				ModelID: "qwen3.5-plus",
			},
		},
	})

	model, _, ok := router.FindModelByID("deepseek-v4-flash")
	if !ok {
		t.Fatal("FindModelByID() = not found, want found")
	}
	if model.ModelID != "qwen3.5-plus" {
		t.Fatalf("ModelID = %q, want %q", model.ModelID, "qwen3.5-plus")
	}
}
