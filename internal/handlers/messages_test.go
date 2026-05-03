package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/syxc/oc-go-cc/internal/config"
	"github.com/syxc/oc-go-cc/internal/transformer"
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

func TestReplaceModelInRawBody_PreservesAllOtherFields(t *testing.T) {
	// Extended body with nested fields, Unicode, and diverse types.
	rawBody := json.RawMessage(`{
  "model": "deepseek-v4-pro",
  "max_tokens": 4096,
  "temperature": 0.7,
  "stream": true,
  "system": "You are a helpful assistant.\nUse 中文 when needed.",
  "messages": [
    {"role": "user", "content": "Hello"},
    {"role": "assistant", "content": "Hi, how can I help?"}
  ],
  "tools": [
    {"name": "read_file", "description": "Read a file", "input_schema": {"type": "object"}}
  ],
  "metadata": {"user_id": "test-user-123"}
}`)

	updated := replaceModelInRawBody(rawBody, "minimax-m2.5")

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Model replaced
	if payload["model"] != "minimax-m2.5" {
		t.Fatalf("model = %q, want %q", payload["model"], "minimax-m2.5")
	}

	// All other fields preserved
	if v, ok := payload["max_tokens"].(float64); !ok || v != 4096 {
		t.Fatalf("max_tokens = %v, want 4096", payload["max_tokens"])
	}
	if v, ok := payload["temperature"].(float64); !ok || v != 0.7 {
		t.Fatalf("temperature = %v, want 0.7", payload["temperature"])
	}
	if v, ok := payload["stream"].(bool); !ok || !v {
		t.Fatalf("stream = %v, want true", payload["stream"])
	}
	if v, ok := payload["system"].(string); !ok || !strings.Contains(v, "中文") {
		t.Fatalf("system = %q, expected Unicode preserved", payload["system"])
	}

	// Messages array preserved
	msgs, ok := payload["messages"].([]interface{})
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %v, want array of 2", payload["messages"])
	}

	// Tools array preserved
	tools, ok := payload["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want array of 1", payload["tools"])
	}

	// Metadata preserved
	meta, ok := payload["metadata"].(map[string]interface{})
	if !ok || meta["user_id"] != "test-user-123" {
		t.Fatalf("metadata = %v, want user_id=test-user-123", payload["metadata"])
	}
}

func TestReplaceModelInRawBody_MalformedJSONFallback(t *testing.T) {
	rawBody := json.RawMessage(`{invalid json`)

	updated := replaceModelInRawBody(rawBody, "minimax-m2.5")

	// Must return the original body unchanged (fallback path)
	if string(updated) != string(rawBody) {
		t.Fatalf("got %q, want original %q", string(updated), string(rawBody))
	}
}

func TestCopyAnthropicStream_EmptyStreamReturnsError(t *testing.T) {
	var out bytes.Buffer
	err := copyAnthropicStream(context.Background(), &out, strings.NewReader(""))
	if err == nil {
		t.Fatal("copyAnthropicStream() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "without any events") {
		t.Fatalf("copyAnthropicStream() error = %q, want to contain %q", err.Error(), "without any events")
	}
}

func TestCopyAnthropicStream_CopiesData(t *testing.T) {
	var out bytes.Buffer
	in := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	err := copyAnthropicStream(context.Background(), &out, strings.NewReader(in))
	if err != nil {
		t.Fatalf("copyAnthropicStream() error = %v", err)
	}
	if out.String() != in {
		t.Fatalf("copyAnthropicStream() output = %q, want %q", out.String(), in)
	}
}

func TestCopyAnthropicStream_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := copyAnthropicStream(ctx, &bytes.Buffer{}, canceledReader{})
	if err != transformer.ErrClientDisconnected {
		t.Fatalf("copyAnthropicStream() error = %v, want %v", err, transformer.ErrClientDisconnected)
	}
}

type canceledReader struct{}

func (canceledReader) Read(_ []byte) (int, error) {
	return 0, context.Canceled
}

func TestNormalizeResponseModel_ClaudePassthrough(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{}}
	tests := []string{
		"claude-sonnet-4-6",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-7",
		"claude-3.5-sonnet",
		"CLAUDE-SONNET-4-6",
	}
	for _, input := range tests {
		got := h.normalizeResponseModel(input, "deepseek-v4-pro")
		if got != input {
			t.Errorf("normalizeResponseModel(%q, _) = %q, want %q", input, got, input)
		}
	}
}

func TestNormalizeResponseModel_HaikuFlashMapsToClaudeHaiku(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{}}
	want := "claude-haiku-4-5-20251001"
	// Names must NOT contain "claude" — those are passthrough before the haiku check.
	tests := []string{"deepseek-v4-flash", "haiku-model", "HAIKU", "flash-lite"}
	for _, input := range tests {
		got := h.normalizeResponseModel(input, "deepseek-v4-pro")
		if got != want {
			t.Errorf("normalizeResponseModel(%q, _) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeResponseModel_OpusMapsToClaudeOpus(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{}}
	// Names must NOT contain "claude" — those are passthrough before the opus check.
	tests := []string{"opus-model", "OPUS"}
	for _, input := range tests {
		got := h.normalizeResponseModel(input, "deepseek-v4-pro")
		if got != "claude-opus-4-7" {
			t.Errorf("normalizeResponseModel(%q, _) = %q, want claude-opus-4-7", input, got)
		}
	}
}

func TestNormalizeResponseModel_DefaultFallbackToSonnet(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{}}
	tests := []string{"deepseek-v4-pro", "qwen3.5-max", "gpt-4o", "unknown-model", ""}
	for _, input := range tests {
		got := h.normalizeResponseModel(input, input)
		if got != "claude-sonnet-4-6" {
			t.Errorf("normalizeResponseModel(%q, _) = %q, want claude-sonnet-4-6", input, got)
		}
	}
}

func TestNormalizeResponseModel_EmptyRequestModelFallsBackToRouted(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{}}
	// When requestModel is empty, routedModel is used for normalization.
	// A Claude routed model should passthrough.
	got := h.normalizeResponseModel("", "claude-sonnet-4-6")
	if got != "claude-sonnet-4-6" {
		t.Errorf("normalizeResponseModel(\"\", \"claude-sonnet-4-6\") = %q, want claude-sonnet-4-6", got)
	}

	// A non-Claude routed model should fall back to sonnet default.
	got = h.normalizeResponseModel("", "deepseek-v4-pro")
	if got != "claude-sonnet-4-6" {
		t.Errorf("normalizeResponseModel(\"\", \"deepseek-v4-pro\") = %q, want claude-sonnet-4-6", got)
	}
}

func TestNormalizeResponseModel_PriorityHaikuOverOpus(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{}}
	// "haiku" check comes before "opus" check in the function.
	// A model containing both should match haiku first.
	got := h.normalizeResponseModel("haiku-opus", "")
	if got != "claude-haiku-4-5-20251001" {
		t.Errorf("normalizeResponseModel(\"haiku-opus\", \"\") = %q, want claude-haiku-4-5-20251001", got)
	}
}

func TestNormalizeResponseModel_ConfigDefaultOverride(t *testing.T) {
	h := &MessagesHandler{config: &config.Config{
		DefaultResponseModel: "claude-haiku-4-5-20251001",
	}}
	// Unknown model should use the configured default, not hardcoded sonnet.
	got := h.normalizeResponseModel("gpt-4o", "gpt-4o")
	if got != "claude-haiku-4-5-20251001" {
		t.Errorf("normalizeResponseModel(%q, _) = %q, want claude-haiku-4-5-20251001", got, got)
	}
}

func TestSendStreamError_ProducesSSEEvent(t *testing.T) {
	h := &MessagesHandler{
		config: &config.Config{},
		logger: slog.Default(),
	}
	w := httptest.NewRecorder()

	// Simulate: headers already sent + keepalive was already flushed
	fmt.Fprintf(w, ":keepalive\n\n")
	w.Flush()

	h.sendStreamError(w, "all upstream models failed")

	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Errorf("missing event: error line in output: %q", body)
	}
	if !strings.Contains(body, "all upstream models failed") {
		t.Errorf("missing error message in output: %q", body)
	}
	if !strings.Contains(body, "api_error") {
		t.Errorf("missing api_error type in output: %q", body)
	}

	// Verify interleaving: keepalive comment should precede error event
	keepalivePos := strings.Index(body, ":keepalive")
	errorPos := strings.Index(body, "event: error")
	if keepalivePos < 0 || errorPos < 0 || keepalivePos >= errorPos {
		t.Errorf("keepalive must precede error event in output: %q", body)
	}
}
