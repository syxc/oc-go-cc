# CLAUDE.md

This file provides guidance to Claude Code when working with this fork.

## Commands

```bash
make build   # Build binary to bin/oc-go-cc
make run     # Run without building
make test    # Run tests with race detector
make lint    # go vet + test
make clean   # Remove build artifacts
make install # go install to $GOPATH/bin
make dist    # Cross-compile for all platforms
```

Install for daily use: `go install ./cmd/oc-go-cc`

## Architecture

**Purpose:** Fork of oc-go-cc optimized for Claude Code + OpenCode Go. Proxies Anthropic API requests through OpenCode Go with model variant routing.

**Model routing is model-name-driven (fork feature),** not content-keyword-driven (upstream). Claude Code sends model variant names (haiku/sonnet/opus) in the request. `FindModelByID()` in `internal/router/model_router.go` maps them to configured scenarios:

- `claude-haiku-*` → background → V4 Flash (cheap/fast)
- `claude-sonnet-*` → default → V4 Pro (balanced)
- `claude-opus-*` → complex → V4 Pro (capable)

**EnableStreamingScenarioRouting** (`config.go`): When true, streaming requests bypass `RouteForStreaming()` and use `DetectScenario()` for content-based routing. Default false for backward compatibility.

**Response model passthrough:** `handleStreaming` and `executeOpenAIRequest` use the original request model name (`anthropicReq.Model`) in responses instead of the routed model ID. This prevents Claude Code's client-side provider routing from failing on unrecognized model names.

**Immediate heartbeat:** Stream handler sends one keepalive immediately before starting the 3-second ticker, reducing cold-start timeout risk.

## Key Files

- `cmd/oc-go-cc/main.go` — CLI entry (cobra), default config template
- `internal/config/config.go` — Config struct with EnableStreamingScenarioRouting
- `internal/router/model_router.go` — FindModelByID (variant mapping), Route, RouteForStreaming
- `internal/router/scenarios.go` — IsToolResult filtering, scenario detection
- `internal/transformer/stream.go` — SSE proxying with content block field fix
- `internal/handlers/messages.go` — Streaming/non-streaming with response model passthrough
- `configs/config.example.json` — Reference config with all options
