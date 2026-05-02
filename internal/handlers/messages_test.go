package handlers

import (
	"encoding/json"
	"testing"
)

func TestReplaceModelInRawBody(t *testing.T) {
	rawBody := json.RawMessage(`{
  "model": "deepseek-v4-pro[1m]",
  "max_tokens": 4096,
  "messages": []
}`)

	updated := replaceModelInRawBody(rawBody, "minimax-m2.5")

	var payload struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
	}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Model != "minimax-m2.5" {
		t.Fatalf("Model = %q, want %q", payload.Model, "minimax-m2.5")
	}
	if payload.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %d, want %d", payload.MaxTokens, 4096)
	}
}
