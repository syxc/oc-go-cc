package config

import "testing"

func TestShouldForwardRaw_ExplicitAnthropicEndpoint(t *testing.T) {
	mc := ModelConfig{ModelID: "some-model", Endpoint: "anthropic"}
	if !mc.ShouldForwardRaw() {
		t.Error("ShouldForwardRaw() = false for Endpoint=anthropic, want true")
	}
}

func TestShouldForwardRaw_ExplicitOpenAIEndpoint(t *testing.T) {
	mc := ModelConfig{ModelID: "minimax-m2.5", Endpoint: "openai"}
	if mc.ShouldForwardRaw() {
		t.Error("ShouldForwardRaw() = true for Endpoint=openai, want false")
	}
}

func TestShouldForwardRaw_EmptyEndpointFallsBackToLegacy(t *testing.T) {
	mc := ModelConfig{ModelID: "minimax-m2.5"}
	if !mc.ShouldForwardRaw() {
		t.Error("ShouldForwardRaw() = false for legacy minimax-m2.5 with empty Endpoint, want true")
	}

	mc2 := ModelConfig{ModelID: "deepseek-v4-pro"}
	if mc2.ShouldForwardRaw() {
		t.Error("ShouldForwardRaw() = true for deepseek-v4-pro with empty Endpoint, want false")
	}
}

func TestShouldForwardRaw_AutoEndpointFallsBackToLegacy(t *testing.T) {
	mc := ModelConfig{ModelID: "minimax-m2.7", Endpoint: "auto"}
	if !mc.ShouldForwardRaw() {
		t.Error("ShouldForwardRaw() = false for auto Endpoint with legacy model, want true")
	}
}

func TestIsLegacyAnthropicModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"minimax-m2.5", true},
		{"minimax-m2.7", true},
		{"deepseek-v4-pro", false},
		{"claude-sonnet-4-6", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsLegacyAnthropicModel(tt.modelID); got != tt.want {
			t.Errorf("IsLegacyAnthropicModel(%q) = %v, want %v", tt.modelID, got, tt.want)
		}
	}
}
