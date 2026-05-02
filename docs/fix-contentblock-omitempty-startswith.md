# Fix: ContentBlock omitempty + 流式 finish 事件丢失导致 Claude Code H.startsWith 错误

- **Branch**: `fix/stream-stability`
- **Date**: 2026-05-02
- **Error**: `API Error: undefined is not an object (evaluating 'H.startsWith')`

## 现象

Claude Code 通过代理请求后，流式响应阶段报错，错误信息为 `undefined is not an object (evaluating 'H.startsWith')`。

该错误是 Claude Code 客户端 JavaScript 的 TypeError，变量 `H` 预期为 string，实际为 `undefined`。

**复现条件**：首次进入 Claude Code 时不手动切换模型，用默认模型请求必现；手动切换到任意模型后正常。

## 根因（两个）

### 根因 1：`omitempty` 丢弃空字符串字段

`ContentBlock` 结构体的 `Text` 和 `Thinking` 字段带有 `omitempty` JSON tag：

```go
type ContentBlock struct {
    Text     string `json:"text,omitempty"`
    Thinking string `json:"thinking,omitempty"`
    // ...
}
```

Go 的 `encoding/json` 在序列化时，空字符串 `""` 被视为零值，`omitempty` 会跳过该字段。

`content_block_start` SSE 事件中，text/thinking 块的初始值为空字符串。序列化后 JSON 中 `text`/`thinking` 字段被丢弃，Claude Code 客户端读到 `undefined`，调用 `startsWith()` 时抛出 TypeError。

**修复**：移除 `ContentBlock.Text` 和 `ContentBlock.Thinking` 的 `omitempty` tag。

### 根因 2（实际主因）：流式 content fast path 吞掉 finish 事件

这是导致默认模型必现、手动切换模型正常的真正原因。

**DeepSeek 的 SSE 行为**：`finish_reason` + `usage` 在最后一个 content chunk 中一起发送，`delta.content` 为空字符串：

```json
data: {"id":"...","choices":[{"delta":{"content":""},"finish_reason":"stop","index":0}],"usage":{"prompt_tokens":123,"completion_tokens":456}}
```

**fast path 的 bug**：`processSSELine` 中 content fast path（`stream.go:202`）的 `return nil` 在 `if content != ""` 块之外，空 content chunk 虽然不发送 delta，但仍触发 early return，导致后续的 `finish_reason` 处理和 JSON 解析路径被跳过：

```go
// 修复前 — return nil 在 if 块外
if idx := strings.Index(data, `"delta":{"content":"`); idx != -1 {
    start := idx + len(`"delta":{"content":"`)
    end := strings.Index(data[start:], `"`)
    if end != -1 {
        content := data[start : start+end]
        if content != "" {
            // ... send content_block_delta
        }
        // 空 content 也 fall through，但没有——return nil 在这里
    }
}
return nil  // ← BUG: 在最外层，空 content 也直接返回
```

**后果**：`content_block_stop` 和 `message_delta`（含 `stop_reason`）永远不会发送。Claude Code 客户端收到的 Anthropic 事件序列不完整，读到 `undefined` 的 stop_reason → `startsWith()` 报错。

**为什么手动切换模型正常**：手动切换后走不同模型/路由路径，上游返回的 SSE chunk 格式不同（`finish_reason` 在独立 chunk 中发送），不走 DeepSeek 的合并格式，不会触发此 bug。

**修复**：将 `return nil` 移入 `if content != ""` 块内，空 content fall through 到 finish_reason / JSON 解析路径。

### 防御措施：stopSent 安全网

部分 provider（如 DeepSeek）可能在 `[DONE]` 前不发送 `finish_reason` chunk。在 `ProxyStream` 主循环结束后、发送 `message_stop` 之前，增加安全网：若 `stopSent == false`，补发 `content_block_stop` + `message_delta`（含 `stop_reason: "end_turn"`）。

```go
if !stopSent {
    if contentStarted || reasoningStarted {
        // 补发 content_block_stop
    }
    // 补发 message_delta with stop_reason
}
```

## 变更文件

| 文件 | 变更 |
|------|------|
| `pkg/types/anthropic.go` | `Text`/`Thinking` 移除 `omitempty` |
| `internal/transformer/stream.go` | 1. fast path `return nil` 移入 `if content != ""` 块内 |
| | 2. `ProxyStream` 末尾增加 `stopSent` 安全网 |
| | 3. `" "` workaround 改回 `""` |
| `internal/transformer/response.go` | `" "` workaround 改回 `""` |

## 修复后正确的 Anthropic SSE 事件序列

```
event: message_start        → 消息开始
event: content_block_start  → 内容块开始（text/thinking）
event: content_block_delta  × N  → 内容增量
event: content_block_stop   → 内容块结束
event: message_delta        → stop_reason + usage
event: message_stop         → 流结束
```

## 排查过程中的回归

### 空响应回归（stray `return nil`）

使用 sed 编辑时，`[DONE]` 处理块后意外遗留一个 `return nil`（`stream.go:195`），导致所有 data 行在 `[DONE]` 之后立即返回空响应。**所有模型**均返回空内容。

修复：删除遗留的 `return nil` 行。

### macOS SIGKILL（exit 137）

`cp` 或 `go build` 输出到 `$GOPATH/bin` 后，未签名的二进制文件被 macOS Gatekeeper 杀掉。

修复：`codesign -s - /Users/syxc/go/bin/oc-go-cc`

## 教训

- **`omitempty` 对空字符串**：Go 的 `omitempty` 对 `string` 类型的 `""` 也会跳过序列化。Anthropic SSE 协议要求部分字段即使为空也必须存在。从类型定义层面根治，不用 workaround。
- **Provider SSE 差异**：不同 provider 的 SSE chunk 格式不同。DeepSeek 合并发送 `finish_reason` + `usage` + 空 `delta.content`，而 OpenAI 分开发送。流式解析必须处理所有变体。
- **fast path 的陷阱**：性能优化路径（字符串匹配而非 JSON 解析）的 early return 必须严格限定在确实处理完数据的分支内，否则会吞掉后续重要事件。
- **防御性设计**：在流结束时检查关键事件是否已发送，补发缺失的事件，是应对 provider 差异的有效策略。
- **sed/自动编辑风险**：复杂多行替换容易引入遗留代码，编辑后必须逐行检查 diff。
