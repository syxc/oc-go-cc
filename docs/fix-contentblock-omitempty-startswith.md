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

---

# Fix Round 2: content_block_stop index 交换 + 空 delta message_delta + safety net 缺少 tool_use 关闭

- **Branch**: `fix/stream-stability`
- **Date**: 2026-05-03
- **Error 1**: `API Error: undefined is not an object (evaluating 'H.startsWith')` (再发)
- **Error 2**: `API Error: Content block not found`

## 根因

### 根因 1：`content_block_stop` index 交换

`contentIndex` 在 text/reasoning 和 tool_use 之间共享递增。典型 text + tool_use 流程：

```
text start:  contentIndex=0, start index=0
tool_use:    contentIndex++ → 1, start index=1
finish 关闭 text:  Index=contentIndex=1  ← 错误！text 启动时 index=0
finish 关闭 tool:  idx=1-1+0=0          ← 错误！tool 启动时 index=1
```

两者索引互换，Claude Code 找不到匹配的 content block。

**修复**：关闭 text/reasoning 时使用 `contentIndex - toolUseCount`（该 block 启动时的 index），关闭 tool_use 时使用 `contentIndex - toolUseCount + i + 1`。

### 根因 2：第二个 `message_delta` 空 delta

当 `finish_reason` 先到达（设置 `stopSent=true`），后续再收到 usage-only chunk 时，代码发送一个 delta 为空的 `message_delta`。Anthropic SSE 协议规定每个 stream 只有一个 `message_delta`。发送第二个（空 delta）可能导致 Claude Code 用 `undefined` 覆盖已接收的 `stop_reason`，触发 H.startsWith。

**修复**：当 `stopSent` 已为 true 时，跳过 usage-only chunk，不发送任何消息。

### 根因 3：safety net 不关闭 tool_use block

`safety net`（`ProxyStream` 末尾的 `!stopSent` 分支）只关闭 text/reasoning block，不处理 tool_use block。当 stream 在 tool_use 后无 `finish_reason` 地结束时，tool_use block 永远不会被关闭。

**修复**：在 safety net 中增加 tool_use block 的关闭逻辑。

## 变更文件

| 文件 | 变更 |
|------|------|
| `internal/transformer/stream.go` | 1. text/reasoning 关闭 index 改为 `contentIndex - toolUseCount`（3 处：finish_reason fast path、JSON path、safety net） |
| | 2. tool_use 关闭 index 改为 `contentIndex - toolUseCount + i + 1`（2 处：JSON path、safety net） |
| | 3. safety net 增加 tool_use block 关闭 |
| | 4. `stopSent==true` usage-only chunk 携带 `stop_reason` |
| | 5. `[DONE]` 空 if 块替换为注释（fix staticcheck） |
| `internal/handlers/messages.go` | fix gofmt indent |
| `pkg/types/anthropic.go` | fix gofmt indent |

---

# Fix Round 3: tool_use 重复创建 content_block_start 导致 H.startsWith + Invalid tool parameters

- **Branch**: `fix/stream-stability`
- **Date**: 2026-05-03
- **Error 1**: `API Error: undefined is not an object (evaluating 'H.startsWith')` (仍出现，4 次)
- **Error 2**: `Invalid tool parameters`

## 现象

H.startsWith 再发，且每次出现 4 次才停下。伴随出现 `Invalid tool parameters`。

## 根因：tool_use 多 chunk 流式协议处理错误

OpenAI 流式格式中，tool_call 数据分多个 chunk 发送：

```
chunk 1: tool_calls[{index:0, id:"call_xxx", function:{name:"read_file", arguments:""}}]
chunk 2: tool_calls[{index:0, function:{arguments:"{\"filePath\":...}"}}]
chunk 3: tool_calls[{index:0, function:{arguments:"...\"}"}}]
```

**bug**：代码对 **每个** chunk 的每个 tool_call 都创建新的 `content_block_start`（`stream.go:419-459`），而非只在首个 chunk（有 `id`）时创建。chunk 2/3 创建的 block 没有 `name`（`tc.Function.Name == ""`）、ID 为随机生成的新值。

结果：
- Claude Code 收到 4 个 tool_use block（1 个正确 + 3 个 name 为空）→ 4 次 `H.startsWith`（`H = name`, `name.startsWith()` 在 `undefined` 上失败）
- 空 name 的 block → `Invalid tool parameters`
- 随机 ID 无法匹配后续 tool_result

**修复**：跟踪已创建的 tool_use block（`toolBlocks map[int]int`，tool call index → content block index）。

- 首个 chunk 有 `tc.ID != ""` 或 `toolBlocks` 中不存在该 index → 创建新 `content_block_start`
- 后续 chunk 无 `tc.ID` 但 index 已在 `toolBlocks` 中 → 只发送 `input_json_delta`

**还需**: 给 `ToolCall` 加 `Index *int json:"index,omitempty"` 字段以解析 OpenAI 流式 tool_call delta 中的 index。

## 变更文件

| 文件 | 变更 |
|------|------|
| `pkg/types/openai.go` | `ToolCall` 加 `Index *int json:"index,omitempty"` |
| `internal/transformer/stream.go` | 1. `ProxyStream` 加 `toolBlocks map[int]int` |
| | 2. `processSSELine` 加 `toolBlocks` 参数 |
| | 3. tool_use 处理: 仅 `tc.ID != ""` 或 index 未跟踪时创建新 block |
