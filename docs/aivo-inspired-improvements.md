# 借鉴 Aivo 架构改进 oc-go-cc 的方案

- **Branch**: `feat/aivo-inspired-improvements`
- **Date**: 2026-05-09
- **Source**: [yuanchuan/aivo](https://github.com/yuanchuan/aivo) — CLI 工具，统一入口连接 Claude Code/Codex/Gemini/Amp 等 agent 到多种 upstream model provider

---

## 背景

Aivo 和 oc-go-cc 都解决了同一个核心问题：**让 Claude Code 的多槽位模型（Haiku / Sonnet / Opus / Reasoning / Subagent）映射到不同的底层模型**。

但两者的架构层级不同：

```
Aivo（进程编排层）：
  CLI 启动 → 注入 env var → Claude Code 直接读 → 直连 upstream

oc-go-cc（HTTP 代理层）：
  CLI 启动 → oc-go-cc serve → Claude Code → proxy 拦截 → 转换/直传 → upstream
```

Aivo 用启动时注入环境变量的方式，零拦截、零格式转换。oc-go-cc 用运行时代理的方式，灵活但引入了 Anthropic↔OpenAI 格式转换的复杂性。

---

## Aivo 的优势（可借鉴的点）

### 1. 多槽位映射完整性

Aivo 的 Claude 槽位映射（`src/services/environment_injector.rs`）：

```rust
const CLAUDE_DEFAULT_MODEL_SLOTS: [&str; 6] = [
    "ANTHROPIC_MODEL",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL",
    "ANTHROPIC_DEFAULT_SONNET_MODEL",
    "ANTHROPIC_DEFAULT_OPUS_MODEL",
    "ANTHROPIC_REASONING_MODEL",     // ← oc-go-cc 缺少
    "CLAUDE_CODE_SUBAGENT_MODEL",
];
```

oc-go-cc 当前映射（`internal/router/model_router.go:136-150`）：

```go
func claudeCodeEnvMappings() []struct { ... } {
    {EnvName: "ANTHROPIC_MODEL",              Scenario: "default"},
    {EnvName: "ANTHROPIC_DEFAULT_HAIKU_MODEL", Scenario: "background"},
    {EnvName: "ANTHROPIC_DEFAULT_SONNET_MODEL", Scenario: "default"},
    {EnvName: "ANTHROPIC_DEFAULT_OPUS_MODEL",  Scenario: "complex"},
    {EnvName: "CLAUDE_CODE_SUBAGENT_MODEL",    Scenario: "background"},
    // 缺少 ANTHROPIC_REASONING_MODEL
}
```

**差距**：缺少 `ANTHROPIC_REASONING_MODEL` → `think` scenario 的映射。

**改进**：补充 `{EnvName: "ANTHROPIC_REASONING_MODEL", Scenario: "think"}`。

---

### 2. 优先使用原生协议，避免不必要的格式转换

Aivo 的核心设计原则（`src/services/ai_launcher.rs`）：

```rust
fn preferred_claude_protocol(base_url: &str) -> ClaudeProviderProtocol {
    // 优先 Anthropic 原生协议
    // 仅对 OpenAI-only 的 upstream 走翻译层
}
```

Aivo 始终优先使用 agent 的原生协议和 API 格式，只有当 upstream 不支持原生协议时才走转换层。

oc-go-cc 当前：

```go
// internal/client/opencode.go:63
func IsAnthropicModel(modelID string) bool {
    switch modelID {
    case "minimax-m2.5", "minimax-m2.7":
        return true
    }
    return false
}
```

**缺陷**：硬编码模型名列表。上游新增 Anthropic-native 模型时需手动更新此函数。

**改进方向**：

1. 按 endpoint URL 判断是否为原生 Anthropic endpoint，而非硬编码模型名
2. 在 config 中让用户声明哪些模型直走 Anthropic endpoint
3. 如果 OpenCode Go 对更多模型也支持 Anthropic endpoint（不仅是 MiniMax），直接透传

```go
// 改进后的思路
type ModelConfig struct {
    ModelID            string `json:"model_id"`
    Endpoint           string `json:"endpoint"` // "openai" | "anthropic" | "auto"
    BaseURL            string `json:"base_url,omitempty"`
    MaxTokens          int    `json:"max_tokens,omitempty"`
    ContextThreshold   int    `json:"context_threshold,omitempty"`
}

func (c *OpenCodeClient) shouldForwardRaw(modelID string) bool {
    // 用户声明了 anthropic endpoint → 直传
    // 否则走 OpenAI 转换
}
```

---

### 3. Key 管理

Aivo 用 AES-256-GCM 加密存储多个 provider 的 API key：

```bash
aivo keys add --name deepseek --base-url https://api.deepseek.com --key sk-xxx
# key 存储在 ~/.config/aivo/config.json，加密后不可读
```

oc-go-cc 当前通过环境变量 `OC_GO_CC_API_KEY` 传递明文 key。

**改进**（低优先级）：
- 如果 oc-go-cc 未来需要管理多个 provider 的 key（不仅仅 OpenCode Go），可借鉴 Aivo 的 key 管理
- 短期可通过 macOS Keychain / Linux secret-tool 存储

---

### 4. 模型列表验证

Aivo 有 `aivo models` 命令，可列出 provider 支持的模型：

```bash
aivo models -s sonnet   # 按名称过滤
aivo models --json      # JSON 输出
```

oc-go-cc 可以添加类似能力：

```bash
oc-go-cc models          # 列出 config 中所有模型及其 scenario 分配
oc-go-cc models --verify # 验证模型 ID 是否在 OpenCode Go 上可用
```

---

## 实施方案

### Phase 1: 补全多槽位映射（低风险，独立改动）

**文件**: `internal/router/model_router.go`

1. 在 `claudeCodeEnvMappings()` 中添加 `ANTHROPIC_REASONING_MODEL → think`
2. 确保 config 中的 `models.think` 已配置默认值
3. 添加测试用例

**影响范围**: 仅 `model_router.go` 一个文件，1-2 行代码改动。

---

### Phase 2: 按 endpoint 类型决定直传 vs 转换（核心改进，中等风险）

**文件**:
- `internal/client/opencode.go` — 新增 `shouldForwardRaw()` 方法
- `internal/config/config.go` — `ModelConfig` 新增 `Endpoint` 字段
- `internal/handlers/messages.go` — 用新方法替代 `IsAnthropicModel()` 调用
- `internal/router/model_router.go` — `FindModelByID()` 返回 endpoint 信息

**设计要点**:

1. `ModelConfig` 新增 `Endpoint` 字段，支持三种值：
   - `"auto"`（默认）— 自动检测，当前只透传 MiniMax
   - `"anthropic"` — 强制直传，不转换
   - `"openai"` — 走完整 Anthropic→OpenAI 转换

2. 向后兼容：`Endpoint` 为空或 `"auto"` 时保持现有行为

3. `scenarios.go` 的 `DetectScenario` 逻辑不受影响

**影响范围**:
- `config.go`: 新增字段（向后兼容）
- `opencode.go`: 新增 `shouldForwardRaw()` 方法
- `messages.go`: 替换 `IsAnthropicModel()` 调用
- `model_router.go`: `RouteResult` 可能需要透传 endpoint 信息

---

### Phase 3: 模型列表验证（可选，低优先级）

**文件**: 新增 `cmd/oc-go-cc/models.go` 或在现有体系添加

**设计**:
1. 读取 config 中的所有 model_id
2. 调用 OpenCode Go 的 `/v1/models`（如果支持）
3. 报告哪些模型可用、哪些不存在

---

## 不变的部分（oc-go-cc 的独特优势）

以下特性是 oc-go-cc 独有的，不需要改动：

| 特性 | 说明 |
|------|------|
| Fallback chain | 模型失败自动切换下一个 |
| Circuit breaker | 跟踪模型健康状态，跳过故障模型 |
| Runtime 热切换 | 改 config 无需重启 |
| Background daemon | `oc-go-cc serve --background` |
| Auto-start on login | launchd (macOS) |
| Token counting | tiktoken 精确统计 |
| SSE streaming | 实时 OpenAI→Anthropic SSE 转换 |
| Rate limiting | 请求限流 |
| Request deduplication | 重复请求去重 |

---

## 风险评估

| Phase | 风险等级 | 原因 |
|-------|---------|------|
| Phase 1 | ✅ 低 | 只添加一行映射，逻辑隔离 |
| Phase 2 | ⚠️ 中 | 涉及 5 个文件，需要向后兼容处理 |
| Phase 3 | ✅ 低 | 纯增量功能，不影响现有路径 |

---

## 参考资料

- [Aivo 源码](https://github.com/yuanchuan/aivo) — `src/services/environment_injector.rs`, `src/services/ai_launcher.rs`
- [Aivo PR #7](https://github.com/yuanchuan/aivo/pull/7) — Amp per-mode model 优先级修复
- [Claude Code Env Vars](https://docs.anthropic.com/en/docs/claude-code/settings#environment-variables)
- [OpenCode Go 文档](https://opencode.ai/docs/go/)
