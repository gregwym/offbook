# ADR 0005: Pluggable AI Providers

## Status
Accepted

## Context
The AI financial assistant should support both cloud (Claude API) and local (Ollama) inference. Users should choose based on their privacy comfort level.

## Decision
Define an `ai.Provider` interface. Implement `ClaudeProvider` and `OllamaProvider`. Users switch between them at runtime via the settings page.

## Rationale
- Maximum user choice: cloud for quality, local for absolute privacy
- Interface-based design prevents vendor lock-in
- Both providers receive the same anonymized context from `context_builder.go`
- Adding future providers (OpenAI, local llama.cpp) requires only implementing the interface

## Consequences
- Must maintain two provider implementations
- Ollama requires the user to install and run Ollama separately
- Claude API requires an API key and internet access
- Context builder must produce provider-agnostic prompts (no Anthropic-specific features in the shared context)
- SSE streaming implementation differs between providers — each handles its own streaming protocol
