# oc-go-cc (Fork 版)

基于 [samueltuyizere/oc-go-cc](https://github.com/samueltuyizere/oc-go-cc) 的 fork 版本，专为 **Claude Code + OpenCode Go** 组合优化。

## 与上游的主要区别

| | 上游 (samueltuyizere) | 本 Fork |
|---|---|---|
| **模型路由** | 内容关键词检测 (hasComplexPattern 等) | 直接识别 Claude Code 模型变体名 (haiku/sonnet/opus) |
| **流式处理** | 流式请求强制路由到 fast 场景，绕过场景配置 | 支持 `enable_streaming_scenario_routing` 配置项，开启后流式也可走场景检测 |
| **响应模型名** | 返回路由后的模型 ID (如 `deepseek-v4-pro`) | 原样返回 Claude Code 请求中的模型名 (如 `claude-opus-4-7`)，避免 Claude Code 客户端 provider 路由失败 |
| **心跳** | Ticker 3 秒后首次发送 | 流开始时立即发送一次心跳，减少冷启动超时 |
| **content_block_start** | `text`/`thinking` 字段为 `""` 空字符串，被 JSON omitempty 删除 | 使用 `" "` 占位，确保字段存在于 JSON 中 |
| **安装方式** | Homebrew (`brew install oc-go-cc`) | Go 原生 (`go install ./cmd/oc-go-cc`) |
| **模块路径** | `oc-go-cc` | `github.com/syxc/oc-go-cc` |

## 适用场景

本 fork 面向使用 **OpenCode Go 订阅** + **Claude Code** 的用户。通过代理将 Claude Code 的 Anthropic API 请求转换为 OpenAI 格式，转发至 OpenCode Go，再将响应转回 Anthropic SSE 格式，实现 Claude Code 的完整体验。

## 安装

### 前置条件

- Go 1.21+
- [OpenCode Go](https://opencode.ai/auth) 订阅和 API Key

### git clone + go install

```bash
git clone https://github.com/syxc/oc-go-cc.git
cd oc-go-cc
go install ./cmd/oc-go-cc
```

安装到 `$GOPATH/bin/oc-go-cc`（已在 PATH 中则全局可用）。

### 更新版本

```bash
cd /path/to/oc-go-cc
git pull
go install ./cmd/oc-go-cc
```

## 快速开始

### 1. 初始化配置

```bash
oc-go-cc init
```

生成默认配置 `~/.config/oc-go-cc/config.json`。

### 2. 设置 API Key

```bash
export OC_GO_CC_API_KEY=sk-opencode-your-key
```

### 3. 启动代理

```bash
oc-go-cc serve                    # 前台运行
oc-go-cc serve --background       # 后台守护进程
oc-go-cc autostart enable         # macOS 开机自启 (launchd)
```

### 4. 配置 Claude Code

在 `~/.claude/settings.json` 中设置：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:3456",
    "ANTHROPIC_AUTH_TOKEN": "unused"
  }
}
```

### 5. 启动 Claude Code

```bash
claude
```

## 配置详解

完整配置路径：`~/.config/oc-go-cc/config.json`

### 推荐配置

```json
{
  "api_key": "${OC_GO_CC_API_KEY}",
  "host": "127.0.0.1",
  "port": 3456,
  "enable_streaming_scenario_routing": true,
  "models": {
    "default": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 8192,
      "reasoning_effort": "high",
      "thinking": { "type": "enabled" }
    },
    "complex": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 8192,
      "reasoning_effort": "max",
      "thinking": { "type": "enabled" }
    },
    "think": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 16384,
      "reasoning_effort": "max",
      "thinking": { "type": "enabled" }
    },
    "long_context": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 16384,
      "context_threshold": 100000,
      "reasoning_effort": "max",
      "thinking": { "type": "enabled" }
    },
    "background": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-flash",
      "temperature": 0.3,
      "max_tokens": 2048
    },
    "fast": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-flash",
      "temperature": 0.7,
      "max_tokens": 4096
    }
  },
  "fallbacks": {
    "default": [
      { "provider": "opencode-go", "model_id": "kimi-k2.6" },
      { "provider": "opencode-go", "model_id": "qwen3.6-plus" },
      { "provider": "opencode-go", "model_id": "deepseek-v4-flash" }
    ],
    "complex": [
      { "provider": "opencode-go", "model_id": "kimi-k2.6" },
      { "provider": "opencode-go", "model_id": "qwen3.6-plus" }
    ],
    "think": [
      { "provider": "opencode-go", "model_id": "deepseek-v4-pro" },
      { "provider": "opencode-go", "model_id": "kimi-k2.6" }
    ],
    "long_context": [
      { "provider": "opencode-go", "model_id": "kimi-k2.5" },
      { "provider": "opencode-go", "model_id": "qwen3.6-plus" }
    ],
    "background": [
      { "provider": "opencode-go", "model_id": "qwen3.5-plus" },
      { "provider": "opencode-go", "model_id": "kimi-k2.5" }
    ],
    "fast": [
      { "provider": "opencode-go", "model_id": "qwen3.5-plus" },
      { "provider": "opencode-go", "model_id": "kimi-k2.5" }
    ]
  },
  "opencode_go": {
    "base_url": "https://opencode.ai/zen/go/v1/chat/completions",
    "anthropic_base_url": "https://opencode.ai/zen/go/v1/messages",
    "timeout_ms": 300000
  },
  "logging": {
    "level": "info",
    "requests": true
  }
}
```

### 模型映射逻辑（核心）

本 fork 的核心路由逻辑：**以 Claude Code 的模型变体名作为路由依据**，而非上游的内容关键词检测。

```
Claude Code 发送的模型名          →  匹配场景        →  实际使用模型
─────────────────────────────────────────────────────────────────
claude-haiku-4-5-xxx              →  background     →  deepseek-v4-flash
claude-sonnet-4-6                 →  default        →  deepseek-v4-pro
claude-opus-4-7                   →  complex        →  deepseek-v4-pro
deepseek-v4-pro (直接匹配)        →  default        →  deepseek-v4-pro
其他不可识别模型名                 →  回退到场景检测  →  按关键词路由
```

匹配顺序：
1. 直接 model_id 匹配（遍历所有场景配置）
2. Claude Code 变体匹配（haiku→background、sonnet→default、opus→complex）
3. 回退：流式场景路由或内容关键词检测

### 配置项说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `api_key` | string | OpenCode Go API Key，支持 `${VAR}` 环境变量插值 |
| `enable_streaming_scenario_routing` | bool | 开启后流式请求也走场景检测（默认关闭，保持与上游兼容） |
| `models.<scenario>` | object | 各场景的模型配置，包含 provider、model_id、temperature、max_tokens 等 |
| `models.<scenario>.reasoning_effort` | string | DeepSeek V4 推理深度，可选 `"high"` 或 `"max"` |
| `models.<scenario>.thinking` | object | DeepSeek V4 思考模式，`{"type":"enabled"}` 或 `{"type":"disabled"}` |
| `models.<scenario>.context_threshold` | int | 触发 long_context 场景的 token 阈值（默认 100000） |
| `fallbacks.<scenario>` | array | 该场景的降级链，按顺序尝试，配合断路器使用 |
| `opencode_go.base_url` | string | OpenAI 兼容端点 URL |
| `opencode_go.anthropic_base_url` | string | Anthropic 兼容端点 URL（用于 MiniMax 等原生 Anthropic 模型） |
| `opencode_go.timeout_ms` | int | 上游超时（毫秒） |
| `logging.level` | string | 日志级别：`debug` / `info` / `warn` / `error` |

### 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `OC_GO_CC_API_KEY` | OpenCode Go API Key（必填） | — |
| `OC_GO_CC_CONFIG` | 自定义配置文件路径 | `~/.config/oc-go-cc/config.json` |
| `OC_GO_CC_HOST` | 代理监听地址 | `127.0.0.1` |
| `OC_GO_CC_PORT` | 代理监听端口 | `3456` |
| `OC_GO_CC_OPENCODE_URL` | OpenCode Go API 端点 | `https://opencode.ai/zen/go/v1/chat/completions` |
| `OC_GO_CC_LOG_LEVEL` | 日志级别 | `info` |

## CLI 命令

```
oc-go-cc serve              # 启动代理
oc-go-cc serve -b          # 后台启动
oc-go-cc serve --port 8080  # 指定端口
oc-go-cc stop               # 停止代理
oc-go-cc status             # 查看状态
oc-go-cc autostart enable   # 开启开机自启 (macOS)
oc-go-cc autostart disable  # 关闭开机自启
oc-go-cc autostart status   # 查看自启状态
oc-go-cc init               # 生成默认配置文件
oc-go-cc validate           # 校验配置文件
oc-go-cc models             # 列出可用模型
oc-go-cc --version          # 版本号
```

## 架构

```
┌─────────────┐     Anthropic API      ┌─────────────┐     OpenAI API       ┌─────────────┐
│  Claude Code ├──────────────────────►│  oc-go-cc    ├────────────────────►│  OpenCode Go │
│  (CLI)       │  POST /v1/messages   │  (Proxy)     │  /chat/completions  │  (Upstream)  │
│              │◄──────────────────────┤              │◄────────────────────┤              │
└─────────────┘   Anthropic SSE        └─────────────┘   OpenAI SSE          └─────────────┘
```

核心模块：

- `cmd/oc-go-cc/main.go` — CLI 入口 (cobra)
- `internal/config/` — 配置加载、`${VAR}` 插值、环境变量覆盖
- `internal/router/` — 模型路由：`FindModelByID`（模型名匹配）→ 降级链 → 场景检测
- `internal/transformer/` — Anthropic ↔ OpenAI 格式转换（请求、响应、流式 SSE）
- `internal/handlers/` — HTTP 处理器：流式 / 非流式聊天、token 计数、健康检查
- `internal/client/` — OpenCode Go HTTP 客户端，双端点（OpenAI / Anthropic）
- `internal/token/` — tiktoken (cl100k_base) token 计数

路由决策优先级：

```
1. 模型名直接匹配 (FindModelByID — direct model_id match)
2. Claude Code 变体匹配 (haiku/sonnet/opus → scenario)
3. 流式且未开启场景路由 → RouteForStreaming (fast)
4. 回退 → DetectScenario (内容关键词)
```

## 上下文大小

Claude Code 自行管理上下文窗口，代理不干预。DeepSeek V4 Pro 实际支持 1M 上下文，Claude Code 按 claude-sonnet/opus 的 200K 管理——对日常编码完全够用。`long_context` 场景在 100K token 时自动触发。

## 已知限制

- **非 DeepSeek 模型需谨慎**：Claude Code 客户端对响应中的模型名有 provider 路由校验。本 fork 已将响应模型名改为原请求模型名，但仍建议主力使用 DeepSeek 系列模型
- **OpenCode Go 使用限制**：5 小时 $12、每周 $30、每月 $60。超出后可在控制台启用余额补充
- **不支持多模态**：上游 oc-go-cc 在 Anthropic→OpenAI 转换时丢弃 image block，需多模态场景请使用 `cc-switch` 切换到 GLM 原生端点

## 许可证

AGPL-3.0（与上游一致）

## 上游

本 fork 基于 [samueltuyizere/oc-go-cc](https://github.com/samueltuyizere/oc-go-cc) v0.0.21，保留了上游的断路降级、SSE 转换、token 计数、后台守护、launchd 自启等全部功能。
