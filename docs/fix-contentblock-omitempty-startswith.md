# Fix: ContentBlock omitempty 导致 Claude Code H.startsWith 错误

- **Branch**: `fix/stream-stability`
- **Date**: 2026-05-02
- **Error**: `API Error: undefined is not an object (evaluating 'H.startsWith')`

## 现象

Claude Code 通过代理请求后，流式响应阶段报错，错误信息为 `undefined is not an object (evaluating 'H.startsWith')`。

该错误是 Claude Code 客户端 JavaScript 的 TypeError，变量 `H` 预期为 string，实际为 `undefined`。

## 根因

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

### 触发路径

1. 上游模型（如 DeepSeek）返回 `reasoning_content` + `text`
2. `processSSELine` 中 fast path 被 `"reasoning_content"` 检查跳过（`stream.go:176`）
3. 进入 JSON 解析路径，文本块触发 `content_block_start` 事件
4. `Text: ""` 被 `omitempty` 丢弃 → JSON 中缺少 `text` 字段
5. Claude Code 客户端读到 `text: undefined` → `H.startsWith()` 报错

## 修复

移除 `ContentBlock.Text` 和 `ContentBlock.Thinking` 的 `omitempty` tag。空字符串正常序列化为 `""`。

### 变更文件

| 文件 | 变更 |
|------|------|
| `pkg/types/anthropic.go` | `Text`/`Thinking` 移除 `omitempty` |
| `internal/transformer/stream.go` | 3 处 `" "` workaround 改回 `""` |
| `internal/transformer/response.go` | 1 处 `" "` workaround 改回 `""` |

### 副作用

移除 `omitempty` 后，`tool_use`/`tool_result` 等块类型在 JSON 中会多出 `"text":"","thinking":""` 两个空字段。经评估：

- Claude Code 客户端忽略多余空字段
- 请求解析路径只读取字段、不重新序列化 ContentBlock
- 测试比较 struct 字段值、不比较原始 JSON 字符串
- Token 计数按 `block.Type` 分支访问字段

无功能影响。

## 修复前排查过程

1. 初步定位为响应 model name 不被 Claude Code 识别 → 添加 `normalizeResponseModel`（commit `3536707`）
2. 发现 `content_block_start` 的 `Text`/`Thinking` 被 `omitempty` 丢弃 → 用 `" "` 替代 `""`（commit `a1a4dcd`）
3. 但仅修复了 fast path（`stream.go:202`），JSON 解析路径（`stream.go:352`）遗漏
4. 最终方案：移除 `omitempty`，彻底消除字段被丢弃的可能

## 教训

- `omitempty` 对 `string` 类型的空字符串 `""` 也会跳过序列化，Anthropic SSE 协议要求部分字段即使为空也必须存在
- workaround（如用 `" "` 替代 `""`）容易在新增代码路径时遗漏，应从类型定义层面根治
