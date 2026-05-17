# replace-ai-with-trpc-agent-go

Migrate AI chat backend from custom http.Client implementation to tRPC-Agent-Go framework. Introduces session memory (SQLite-backed), structured tool calling (search funds, market quotes), agent runtime, while keeping multi-provider routing (DeepSeek/Qwen/...) via OpenAI-compatible model adapter. One-shot replacement, no backward compatibility for old AI sessions.
