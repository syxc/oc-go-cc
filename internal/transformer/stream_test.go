package transformer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/syxc/oc-go-cc/pkg/types"
)

// mockResponseWriter implements http.ResponseWriter and http.Flusher for testing.
type mockResponseWriter struct {
	buf    bytes.Buffer
	header http.Header
	status int
}

func newMockResponseWriter() *mockResponseWriter {
	return &mockResponseWriter{
		header: make(http.Header),
	}
}

func (m *mockResponseWriter) Header() http.Header         { return m.header }
func (m *mockResponseWriter) Write(p []byte) (int, error) { return m.buf.Write(p) }
func (m *mockResponseWriter) WriteHeader(statusCode int)  { m.status = statusCode }
func (m *mockResponseWriter) Flush()                      {}

// sseLines builds raw SSE body from a list of data payloads.
func sseLines(lines ...string) io.ReadCloser {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

// parseSSEEvents parses the raw response buffer into a slice of MessageEvent.
func parseSSEEvents(t *testing.T, raw string) []types.MessageEvent {
	t.Helper()
	var events []types.MessageEvent
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "" || data == "[DONE]" {
				continue
			}
			var ev types.MessageEvent
			if err := json.Unmarshal([]byte(data), &ev); err != nil {
				t.Fatalf("unmarshal SSE event: %v (data: %s)", err, data)
			}
			events = append(events, ev)
		}
	}
	return events
}

func TestProxyStream_ReasoningContentFastPath(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		`{"choices":[{"delta":{"reasoning_content":"Let me think"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":" step by step"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "kimi-k2.6", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Expected: message_start, content_block_start, 2x content_block_delta, content_block_stop, message_delta, message_stop
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d: %+v", len(events), events)
	}

	if events[0].Type != "message_start" {
		t.Errorf("event[0].Type = %q, want message_start", events[0].Type)
	}
	if events[1].Type != "content_block_start" {
		t.Errorf("event[1].Type = %q, want content_block_start", events[1].Type)
	}
	if events[1].ContentBlock == nil || events[1].ContentBlock.Type != "thinking" {
		t.Errorf("event[1].ContentBlock = %+v, want thinking block", events[1].ContentBlock)
	}
	if events[2].Type != "content_block_delta" {
		t.Errorf("event[2].Type = %q, want content_block_delta", events[2].Type)
	}
	if got := events[2].Delta.Type; got != "thinking_delta" {
		t.Errorf("event[2].Delta.Type = %q, want thinking_delta", got)
	}
	if got := events[2].Delta.Thinking; got != "Let me think" {
		t.Errorf("event[2].Delta.Thinking = %q, want %q", got, "Let me think")
	}
	if events[3].Type != "content_block_delta" {
		t.Errorf("event[3].Type = %q, want content_block_delta", events[3].Type)
	}
	if got := events[3].Delta.Thinking; got != " step by step" {
		t.Errorf("event[3].Delta.Thinking = %q, want %q", got, " step by step")
	}
	if events[4].Type != "content_block_stop" {
		t.Errorf("event[4].Type = %q, want content_block_stop", events[4].Type)
	}
	if events[5].Type != "message_delta" {
		t.Errorf("event[5].Type = %q, want message_delta", events[5].Type)
	}
	if events[6].Type != "message_stop" {
		t.Errorf("event[6].Type = %q, want message_stop", events[6].Type)
	}
}

func TestProxyStream_ReasoningThenText(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		`{"choices":[{"delta":{"reasoning_content":"Thinking..."}}]}`,
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "kimi-k2.6", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Expected: message_start, content_block_start(thinking, idx=0), thinking_delta, content_block_stop(idx=0),
	//           content_block_start(text, idx=1), text_delta x2, content_block_stop(idx=1), message_delta, message_stop
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d: %+v", len(events), events)
	}

	// Verify indexes
	if got := *events[1].Index; got != 0 {
		t.Errorf("thinking start index = %d, want 0", got)
	}
	if got := *events[3].Index; got != 0 {
		t.Errorf("thinking stop index = %d, want 0", got)
	}
	if got := *events[4].Index; got != 1 {
		t.Errorf("text start index = %d, want 1", got)
	}
	if got := *events[7].Index; got != 1 {
		t.Errorf("text stop index = %d, want 1", got)
	}

	// Verify types
	if events[1].ContentBlock == nil || events[1].ContentBlock.Type != "thinking" {
		t.Errorf("event[1].ContentBlock = %+v, want thinking block", events[1].ContentBlock)
	}
	if got := events[2].Delta.Type; got != "thinking_delta" {
		t.Errorf("event[2].Delta.Type = %q, want thinking_delta", got)
	}
	if events[4].ContentBlock == nil || events[4].ContentBlock.Type != "text" {
		t.Errorf("event[4].ContentBlock = %+v, want text block", events[4].ContentBlock)
	}
	if got := events[5].Delta.Type; got != "text_delta" {
		t.Errorf("event[5].Delta.Type = %q, want text_delta", got)
	}
}

func TestProxyStream_TextOnlyStillWorks(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "kimi-k2.6", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Expected: message_start, content_block_start, 2x content_block_delta, content_block_stop, message_delta, message_stop
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d: %+v", len(events), events)
	}

	if events[1].Type != "content_block_start" || events[1].ContentBlock == nil || events[1].ContentBlock.Type != "text" {
		t.Errorf("event[1] = %+v, want content_block_start(text)", events[1])
	}
	if events[2].Type != "content_block_delta" || events[2].Delta.Type != "text_delta" {
		t.Errorf("event[2] = %+v, want content_block_delta(text_delta)", events[2])
	}
	if events[2].Delta.Text != "Hello" {
		t.Errorf("event[2].Delta.Text = %q, want Hello", events[2].Delta.Text)
	}
}

func TestProxyStream_UsageOnlyChunk(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168,"prompt_cache_hit_tokens":100,"prompt_cache_miss_tokens":23}}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())
	var usage *types.Usage
	for _, event := range events {
		if event.Usage != nil {
			usage = event.Usage
		}
	}
	if usage == nil {
		t.Fatalf("no usage event found in stream: %+v", events)
	}
	if got, want := usage.InputTokens, 123; got != want {
		t.Fatalf("InputTokens = %d, want %d", got, want)
	}
	if got, want := usage.OutputTokens, 45; got != want {
		t.Fatalf("OutputTokens = %d, want %d", got, want)
	}
	if got, want := usage.CacheReadInputTokens, 100; got != want {
		t.Fatalf("CacheReadInputTokens = %d, want %d", got, want)
	}
	if got, want := usage.CacheCreationInputTokens, 23; got != want {
		t.Fatalf("CacheCreationInputTokens = %d, want %d", got, want)
	}
}

// TestProxyStream_NoDuplicateMessageDelta verifies that when finish_reason and
// usage arrive in separate chunks, both message_delta events carry a stop_reason
// so the client never sees an undefined stop_reason (H.startsWith). Usage is
// carried alongside the second stop_reason.
func TestProxyStream_NoDuplicateMessageDelta(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Both message_delta events must carry a stop_reason so the client
	// never sees undefined stop_reason (H.startsWith root cause).
	var stopDeltas []types.MessageEvent
	for _, ev := range events {
		if ev.Type == "message_delta" && ev.Delta != nil && ev.Delta.StopReason != "" {
			stopDeltas = append(stopDeltas, ev)
		}
	}

	if len(stopDeltas) != 2 {
		t.Fatalf("expected exactly 2 message_delta events with stop_reason, got %d: %+v", len(stopDeltas), stopDeltas)
	}

	// The second message_delta should carry usage
	if stopDeltas[1].Usage == nil {
		t.Fatalf("second message_delta should carry usage: %+v", stopDeltas[1])
	}
	if got, want := stopDeltas[1].Usage.InputTokens, 100; got != want {
		t.Errorf("InputTokens = %d, want %d", got, want)
	}

	// Verify usage is somewhere in the stream
	var totalUsage *types.Usage
	for _, ev := range events {
		if ev.Usage != nil {
			totalUsage = ev.Usage
		}
	}
	if totalUsage == nil {
		t.Fatalf("no usage found in stream: %+v", events)
	}
	if got, want := totalUsage.InputTokens, 100; got != want {
		t.Errorf("InputTokens = %d, want %d", got, want)
	}
}

func TestProxyStream_ReasoningJSONFallback(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// This payload does NOT match the fast-path string pattern because of extra whitespace,
	// forcing the JSON fallback path.
	body := sseLines(
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{ReasoningContent: strPtr("Reasoning via JSON")})),
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "kimi-k2.6", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Expected: message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d: %+v", len(events), events)
	}

	if events[1].Type != "content_block_start" || events[1].ContentBlock == nil || events[1].ContentBlock.Type != "thinking" {
		t.Errorf("event[1] = %+v, want content_block_start(thinking)", events[1])
	}
	if events[2].Type != "content_block_delta" || events[2].Delta.Type != "thinking_delta" {
		t.Errorf("event[2] = %+v, want content_block_delta(thinking_delta)", events[2])
	}
	if events[2].Delta.Thinking != "Reasoning via JSON" {
		t.Errorf("event[2].Delta.Thinking = %q, want %q", events[2].Delta.Thinking, "Reasoning via JSON")
	}
}

func TestProxyStream_EmptyReasoningContentSkipped(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{ReasoningContent: strPtr("")})),
		`{"choices":[{"delta":{"content":"Only text"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "kimi-k2.6", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Empty reasoning should be skipped; only one text chunk -> 6 events total
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d: %+v", len(events), events)
	}

	if events[1].Type != "content_block_start" || events[1].ContentBlock == nil || events[1].ContentBlock.Type != "text" {
		t.Errorf("event[1] = %+v, want content_block_start(text)", events[1])
	}
	if *events[1].Index != 0 {
		t.Errorf("text start index = %d, want 0", *events[1].Index)
	}
}

func TestProxyStream_ReasoningAndContentInSameChunk(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	body := sseLines(
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ReasoningContent: strPtr("Thinking..."),
			Content:          "Hello",
		})),
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "kimi-k2.6", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + thinking_start + thinking_delta + thinking_stop +
	// text_start + text_delta("Hello") + text_delta(" world") + text_stop +
	// message_delta + message_stop = 10
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d: %+v", len(events), events)
	}

	// Block 0: thinking
	if events[1].Type != "content_block_start" || events[1].ContentBlock == nil || events[1].ContentBlock.Type != "thinking" {
		t.Errorf("event[1] = %+v, want content_block_start(thinking)", events[1])
	}
	if events[2].Type != "content_block_delta" || events[2].Delta.Type != "thinking_delta" {
		t.Errorf("event[2] = %+v, want content_block_delta(thinking_delta)", events[2])
	}
	if events[2].Delta.Thinking != "Thinking..." {
		t.Errorf("event[2].Delta.Thinking = %q, want %q", events[2].Delta.Thinking, "Thinking...")
	}
	if events[3].Type != "content_block_stop" {
		t.Errorf("event[3].Type = %q, want content_block_stop", events[3].Type)
	}

	// Block 1: text
	if events[4].Type != "content_block_start" || events[4].ContentBlock == nil || events[4].ContentBlock.Type != "text" {
		t.Errorf("event[4] = %+v, want content_block_start(text)", events[4])
	}
	if events[5].Type != "content_block_delta" || events[5].Delta.Type != "text_delta" {
		t.Errorf("event[5] = %+v, want content_block_delta(text_delta)", events[5])
	}
	if events[5].Delta.Text != "Hello" {
		t.Errorf("event[5].Delta.Text = %q, want Hello", events[5].Delta.Text)
	}
	if events[6].Type != "content_block_delta" || events[6].Delta.Type != "text_delta" {
		t.Errorf("event[6] = %+v, want content_block_delta(text_delta)", events[6])
	}
	if events[6].Delta.Text != " world" {
		t.Errorf("event[6].Delta.Text = %q, want \" world\"", events[6].Delta.Text)
	}
	if events[7].Type != "content_block_stop" {
		t.Errorf("event[7].Type = %q, want content_block_stop", events[7].Type)
	}
}

// TestProxyStream_ReasoningBeforeContentFastPathRegression ensures that when
// a provider sends reasoning_content BEFORE content in the same delta (with no
// role field), the fast path for content is skipped and reasoning_content is
// not silently dropped. If it were dropped, the next turn would fail on
// DeepSeek with "reasoning_content must be passed back".
func TestProxyStream_ReasoningBeforeContentFastPathRegression(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Hand-crafted JSON: reasoning_content appears before content, no role field.
	// Before the fix, the fast path matched "delta":{"content":" and returned
	// early, discarding reasoning_content entirely.
	body := sseLines(
		`{"choices":[{"delta":{"reasoning_content":"Thinking...","content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-flash", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + thinking_start + thinking_delta + thinking_stop +
	// text_start + text_delta("Hello") + text_delta(" world") + text_stop +
	// message_delta + message_stop = 10
	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d: %+v", len(events), events)
	}

	// Block 0: thinking (must NOT be lost)
	if events[1].Type != "content_block_start" || events[1].ContentBlock == nil || events[1].ContentBlock.Type != "thinking" {
		t.Errorf("event[1] = %+v, want content_block_start(thinking)", events[1])
	}
	if events[2].Type != "content_block_delta" || events[2].Delta.Type != "thinking_delta" {
		t.Errorf("event[2] = %+v, want content_block_delta(thinking_delta)", events[2])
	}
	if events[2].Delta.Thinking != "Thinking..." {
		t.Errorf("event[2].Delta.Thinking = %q, want %q", events[2].Delta.Thinking, "Thinking...")
	}

	// Block 1: text
	if events[4].Type != "content_block_start" || events[4].ContentBlock == nil || events[4].ContentBlock.Type != "text" {
		t.Errorf("event[4] = %+v, want content_block_start(text)", events[4])
	}
	if events[5].Delta.Text != "Hello" {
		t.Errorf("event[5].Delta.Text = %q, want Hello", events[5].Delta.Text)
	}
}

func TestProxyStream_SafetyNetTextWithoutFinishReason(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// DeepSeek-style: content chunks end, then [DONE] without finish_reason.
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
		`[DONE]`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + content_block_start + 2x content_block_delta +
	// content_block_stop (safety net) + message_delta (safety net) + message_stop = 7
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d: %+v", len(events), events)
	}

	// Safety net content_block_stop
	stopIdx := 5
	if events[stopIdx-1].Type != "content_block_stop" {
		t.Errorf("event[%d].Type = %q, want content_block_stop", stopIdx-1, events[stopIdx-1].Type)
	}
	// Safety net message_delta must carry stop_reason
	deltaIdx := 5
	if events[deltaIdx].Type != "message_delta" {
		t.Errorf("event[%d].Type = %q, want message_delta", deltaIdx, events[deltaIdx].Type)
	}
	if events[deltaIdx].Delta == nil || events[deltaIdx].Delta.StopReason != "end_turn" {
		t.Errorf("event[%d].Delta = %+v, want stop_reason=end_turn", deltaIdx, events[deltaIdx].Delta)
	}
	// message_stop at the end
	if events[6].Type != "message_stop" {
		t.Errorf("event[6].Type = %q, want message_stop", events[6].Type)
	}
}

func TestProxyStream_SafetyNetToolUseWithoutFinishReason(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Stream with text + tool_use, ending with [DONE] and no finish_reason.
	body := sseLines(
		`{"choices":[{"delta":{"content":"Let me read that file."}}]}`,
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{ID: "call_123", Index: intPtr(0), Function: types.FunctionCall{
					Name:      "read_file",
					Arguments: `{"filePath":"/` + `tmp/test.go"}`,
				}},
			},
		})),
		`[DONE]`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + content_block_start(text) + text_delta + content_block_start(tool) +
	// input_json_delta + content_block_stop(text, safety net) +
	// content_block_stop(tool, safety net) + message_delta(safety net) + message_stop = 9
	if len(events) != 9 {
		t.Fatalf("expected 9 events, got %d: %+v", len(events), events)
	}

	// Verify text block stop from safety net
	if events[5].Type != "content_block_stop" || events[5].Index == nil || *events[5].Index != 0 {
		t.Errorf("event[5] = %+v, want content_block_stop index=0", events[5])
	}
	// Verify tool_use block stop from safety net
	if events[6].Type != "content_block_stop" || events[6].Index == nil || *events[6].Index != 1 {
		t.Errorf("event[6] = %+v, want content_block_stop index=1", events[6])
	}
	// Safety net message_delta
	if events[7].Type != "message_delta" || events[7].Delta == nil || events[7].Delta.StopReason != "end_turn" {
		t.Errorf("event[7] = %+v, want message_delta with stop_reason=end_turn", events[7])
	}
	if events[8].Type != "message_stop" {
		t.Errorf("event[8].Type = %q, want message_stop", events[8].Type)
	}
}

func TestProxyStream_SafetyNetOnlyFiresWhenStopNotSent(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Normal stream with finish_reason — safety net must NOT add duplicate events.
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + content_block_start + text_delta +
	// content_block_stop (fast path) + message_delta (fast path) + message_stop = 6
	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d: %+v", len(events), events)
	}
}

func TestProxyStream_ToolUseMultiChunkDedup(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// OpenAI-style multi-chunk tool call: chunk 1 has ID+Name+args,
	// chunk 2 has only args (no ID). Dedup must NOT create duplicate blocks.
	body := sseLines(
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{ID: "call_abc", Index: intPtr(0), Function: types.FunctionCall{
					Name:      "read_file",
					Arguments: `{"filePath":"/` + `tmp/x.go"}`,
				}},
			},
		})),
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{Index: intPtr(0), Function: types.FunctionCall{
					Arguments: `}` + `}`,
				}},
			},
		})),
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{Index: intPtr(0), Function: types.FunctionCall{
					Arguments: `,` + `"mode":"r"}`,
				}},
			},
		})),
		`{"choices":[{"delta":{},"finish_reason":"stop","usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + content_block_start(tool) + 3x input_json_delta +
	// content_block_stop(tool) + message_delta + message_stop = 8
	if len(events) != 8 {
		t.Fatalf("expected 8 events, got %d: %+v", len(events), events)
	}

	// Only one content_block_start for the tool
	startCount := 0
	for _, ev := range events {
		if ev.Type == "content_block_start" && ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
			startCount++
		}
	}
	if startCount != 1 {
		t.Fatalf("expected exactly 1 tool_use content_block_start, got %d", startCount)
	}

	// All tool deltas must be input_json_delta
	for _, ev := range events {
		if ev.Type == "content_block_delta" && ev.Delta != nil {
			if ev.Delta.Type != "input_json_delta" {
				t.Errorf("unexpected delta type %q, want input_json_delta", ev.Delta.Type)
			}
		}
	}
}

func TestProxyStream_TwoToolCallsMultiChunkDedup(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Two different tool calls (index 0 and 1), each arriving in multiple chunks.
	body := sseLines(
		// Tool A chunk 1: ID, name, partial args
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{ID: "call_a", Index: intPtr(0), Function: types.FunctionCall{
					Name:      "read_file",
					Arguments: `{"f":"x"`,
				}},
			},
		})),
		// Tool A chunk 2: more args
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{Index: intPtr(0), Function: types.FunctionCall{
					Arguments: `}`,
				}},
			},
		})),
		// Tool B chunk 1: ID, name, partial args
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{ID: "call_b", Index: intPtr(1), Function: types.FunctionCall{
					Name:      "write_file",
					Arguments: `{"f":"y"`,
				}},
			},
		})),
		// Tool B chunk 2: more args
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{Index: intPtr(1), Function: types.FunctionCall{
					Arguments: `}`,
				}},
			},
		})),
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// message_start + 2x content_block_start(tool A + tool B) +
	// 4x input_json_delta (2 per tool) + 2x content_block_stop (A + B) +
	// message_delta + message_stop = 11
	if len(events) != 11 {
		t.Fatalf("expected 11 events, got %d: %+v", len(events), events)
	}

	// Exactly 2 tool content_block_start events with correct names
	var toolStarts []types.MessageEvent
	for _, ev := range events {
		if ev.Type == "content_block_start" && ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
			toolStarts = append(toolStarts, ev)
		}
	}
	if len(toolStarts) != 2 {
		t.Fatalf("expected 2 tool_use content_block_start, got %d: %+v", len(toolStarts), toolStarts)
	}
	if toolStarts[0].ContentBlock.Name != "read_file" {
		t.Errorf("tool A Name = %q, want read_file", toolStarts[0].ContentBlock.Name)
	}
	if toolStarts[1].ContentBlock.Name != "write_file" {
		t.Errorf("tool B Name = %q, want write_file", toolStarts[1].ContentBlock.Name)
	}
}

func TestProxyStream_ToolUseNilIndexDefaultsToZero(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Tool call without explicit Index field — should default to 0.
	body := sseLines(
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{ID: "call_x", Function: types.FunctionCall{
					Name:      "search",
					Arguments: `{"q":"hello"}`,
				}},
			},
		})),
		// Second chunk for same tool call (no ID, no Index)
		fmt.Sprintf(`{"choices":[{"delta":%s}]}`, mustJSON(t, types.ChatMessage{
			ToolCalls: []types.ToolCall{
				{Function: types.FunctionCall{
					Arguments: `,` + `"limit":10}`,
				}},
			},
		})),
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Only one content_block_start for the tool
	startCount := 0
	for _, ev := range events {
		if ev.Type == "content_block_start" && ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
			startCount++
		}
	}
	if startCount != 1 {
		t.Fatalf("expected exactly 1 tool_use content_block_start, got %d", startCount)
	}
}

func TestProxyStream_ContentFastPathUnescapeValid(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Valid JSON escape sequences: \n, \t, \\, \" should be unescaped correctly.
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello\nWorld"}}]}`,
		`{"choices":[{"delta":{"content":"\tIndented"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Find text deltas
	var texts []string
	for _, ev := range events {
		if ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" {
			texts = append(texts, ev.Delta.Text)
		}
	}
	if len(texts) != 2 {
		t.Fatalf("expected 2 text_delta events, got %d: %q", len(texts), texts)
	}

	// \n should be actual newline, not literal \n
	if texts[0] != "Hello\nWorld" {
		t.Errorf("text[0] = %q, want %q", texts[0], "Hello\nWorld")
	}
	if texts[1] != "\tIndented" {
		t.Errorf("text[1] = %q, want %q", texts[1], "\tIndented")
	}
}

func TestProxyStream_ContentFastPathMalformedEscapeFallback(t *testing.T) {
	handler := NewStreamHandler()
	w := newMockResponseWriter()
	// Malformed escape sequence \z — unescape fails, falls back to raw string.
	body := sseLines(
		`{"choices":[{"delta":{"content":"Hello\\zWorld"}}]}`,
		`{"choices":[{"delta":{"content":" more"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := handler.ProxyStream(w, body, "deepseek-v4-pro", ctx); err != nil {
		t.Fatalf("ProxyStream error: %v", err)
	}

	events := parseSSEEvents(t, w.buf.String())

	// Must have at least message_start, content_block_start, 2x delta, stop, message_delta, message_stop
	if len(events) < 6 {
		t.Fatalf("expected at least 6 events, got %d: %+v", len(events), events)
	}

	// First text delta should contain the raw content (fallback path)
	var texts []string
	for _, ev := range events {
		if ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" {
			texts = append(texts, ev.Delta.Text)
		}
	}
	if len(texts) != 2 {
		t.Fatalf("expected 2 text_delta events, got %d: %q", len(texts), texts)
	}

	// Falls back to raw string; \x remains literal
	if !strings.Contains(texts[0], `\z`) {
		t.Errorf("text[0] = %q, expected literal \\z fallback", texts[0])
	}
}

// helpers

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func strPtr(s string) *string  { return &s }
func intPtr(i int) *int        { return &i }
