# AgentGate Product Strategy

## Vision

AgentGate is the open-source safety control plane for autonomous software operations:

- Before command execution: predict risk and enforce policy.
- During execution: gate destructive operations with deterministic controls.
- After execution: produce auditable evidence and metrics.

## What is implemented

1. Broader safety coverage:
   - Infra tools: `kubectl`, `terraform`, `helm`, `aws`, `gcloud`
   - Developer/local tools: `git`, `docker`
   - MCP proxy: intercept any MCP server tool calls
2. Preflight simulation:
   - `agentgate explain <tool> -- <args...>`
   - Returns environment detection, policy match, risk, and effective decision.
3. Higher precision policy model:
   - `resource_name` matching for branch/resource-level rules.
   - `mcp_server`, `mcp_tool`, `mcp_args_contain` for MCP tool call matching.
4. Default safety policies:
   - force-push protections
   - hard-reset protections
   - docker prune/volume teardown controls
   - MCP file deletion and destructive SQL blocking

## Why AgentGate stands out

- Most guardrails focus only on cloud infra APIs; AgentGate also protects local destructive paths where AI agents often operate.
- MCP proxy extends the same policy engine to AI agent tool calls — no other open-source tool does this.
- Uniform policy model across infra + local tooling + MCP reduces fragmentation.
- Explainability output (`agentgate explain`) improves trust and debugging for human + AI workflows.
- Local-first architecture with zero external dependencies lowers adoption friction.

## Metrics to prove value

- blocked destructive commands per week
- near-miss rate (confirm + deny counts)
- mean time to safe resolution for blocked actions
- policy coverage growth (% commands parsed vs unknown)
- false-positive override rate

## Roadmap

1. Policy UX and safety depth:
   - richer rule conditions (time windows, branch patterns, actor tags)
   - policy test command with fixtures
   - policy negation rules and regex matching
2. Integrations:
   - Prometheus/Grafana starter pack
   - GitHub Actions and CI wrappers
   - `agentgate mcp-init` to auto-wrap all configured MCP servers
3. Distribution:
   - Homebrew formula
   - goreleaser binaries
   - Install script
4. Community:
   - Shared policy library (community-contributed policy packs)
   - Plugin system for custom parsers
