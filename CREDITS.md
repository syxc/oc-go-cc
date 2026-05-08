# Credits

## Upstream Project

This project is a fork of **[oc-go-cc](https://github.com/samueltuyizere/oc-go-cc)** by **Samuel Tuyizere ([@samueltuyizere](https://github.com/samueltuyizere))**.

Samuel built the original proxy that connects Claude Code to OpenCode Go — the entire Anthropic↔OpenAI transformation layer, model routing, fallback chains, circuit breaker, and streaming infrastructure. Without his work, this fork wouldn't exist.

This fork is maintained independently for personal daily use. It is not intended to be merged back upstream due to significant architectural divergence.

## Motivation

The upstream project is solid. The fork exists to:

1. **Fix streaming stability issues** encountered in daily Claude Code usage
2. **Adopt Aivo's architecture principles** — prefer native protocol passthrough over Anthropic↔OpenAI conversion where possible
3. **Maintain stricter cost-aware routing** tailored to personal OpenCode Go usage patterns

## License

This project inherits the original [GNU Affero General Public License v3.0](LICENSE) from upstream.
