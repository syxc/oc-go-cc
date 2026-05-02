package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

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
