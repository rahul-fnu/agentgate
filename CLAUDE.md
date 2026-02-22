# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AgentGate is a local policy-enforcement shim for infrastructure CLIs (`kubectl`, `terraform`, `helm`, `aws`, `gcloud`). It wraps CLI calls via PATH shims, evaluates YAML-based policies, and enforces decisions (`allow`, `warn`, `confirm`, `deny`) — entirely file-based, no server or database required.

## Build & Development Commands

```bash
# Format code
gofmt -w ./cmd ./internal

# Build binary
go build -o agentgate ./cmd/agentgate

# Tidy dependencies
go mod tidy

# Initialize shims and config
./agentgate init
export PATH="$HOME/.agentgate/bin:$PATH"

# Switch enforcement modes
./agentgate observe   # simulate decisions (safe)
./agentgate enforce   # real enforcement
```

There are no automated tests — the project is tested manually by running CLI commands through the shims.

## Architecture

### Execution Flow

When a user runs `kubectl delete namespace foo`:
1. The shim at `~/.agentgate/bin/kubectl` calls `agentgate __intercept kubectl -- delete namespace foo`
2. `main.go` routes `__intercept` to `Intercept()` in `intercept.go`
3. `parser.go` extracts action/resource/action_type from raw args
4. `environment.go` detects the environment (production/staging/dev) from kubeconfig context, terraform workspace, AWS profile, or gcloud config
5. `policy.go` loads `~/.agentgate/policies.yaml` and evaluates matching rules by priority
6. `bypass.go` checks for a valid one-time token
7. The decision is applied: allow (exec real binary), warn (exec + stderr warning), confirm (interactive prompt), or deny (exit 77)
8. Events are logged to `~/.agentgate/events.jsonl`

### Key Files

| File | Responsibility |
|------|---------------|
| `cmd/agentgate/main.go` | Entry point; routes CLI vs `__intercept` path |
| `internal/agentgate/types.go` | Core data structures (`CommandContext`, `Policy`, `Decision`, etc.) |
| `internal/agentgate/intercept.go` | Main interception flow and decision application |
| `internal/agentgate/policy.go` | Policy matching and evaluation engine |
| `internal/agentgate/parser.go` | Tool-specific CLI argument parsers |
| `internal/agentgate/environment.go` | Environment detection with 30s cache |
| `internal/agentgate/cli.go` | User-facing CLI commands (`init`, `status`, `tail`, `report`, `allow-once`, etc.) |
| `internal/agentgate/logging.go` | Append-only JSONL event logging with auto-rotation at 50MB |
| `internal/agentgate/bypass.go` | One-time bypass token issue/consume (5-min TTL, 1 use) |
| `internal/agentgate/config.go` | File I/O for config/policies, path resolution under `~/.agentgate/` |
| `internal/agentgate/starter_policies.go` | Default policy YAML embedded in source |

### Data Directory (`~/.agentgate/`)

- `config.yaml` — mode (observe/enforce), unknown_env_default_decision
- `policies.yaml` — YAML rules
- `events.jsonl` — append-only event log (start/end pairs)
- `history.jsonl` — used for rate-limit and require-plan checks
- `bypasses.jsonl` — bypass token records
- `cache.json` — environment detection cache
- `bin/` — shim scripts for each wrapped CLI tool

### Policy Matching

Policies match on: `tool`, `environment`, `action`, `action_type`, `resource`, `namespace`, `flags`, `raw_contains`. Non-wildcard patterns use substring matching; wildcards use `filepath.Match` semantics (e.g., `*prod*`). The highest-priority matching policy wins.

Advanced rule types:
- **rate_limit** — count writes/destructive ops in a time window (uses `history.jsonl`)
- **require_plan** — require a recent `terraform plan` before `terraform apply`

### Stderr Contract

For every intercepted command, AgentGate writes structured key=value lines to stderr that agents/wrappers can parse:

```
AGENTGATE_ACTIVE=true
AGENTGATE_DECISION=<allow|warn|confirm|deny>
AGENTGATE_POLICY=<policy-name>
AGENTGATE_RISK=<1-99>
AGENTGATE_ENVIRONMENT=<production|staging|dev|unknown>
AGENTGATE_COMMAND_ID=<ag_xxxxxxxx>
AGENTGATE_CONFIRM_REQUIRED=<true|false>
AGENTGATE_SUGGESTION=<message>
```

### Fail-Open Design

AgentGate is intentionally permissive on errors: parse failures, config load errors, and environment detection failures all default to safe values and allow the command through rather than blocking. Info/help flags (`--help`, `-h`, `--version`, etc.) bypass policy entirely.

### Risk Scoring

Risk score (1–99) = base (10) + action_type (read +5, write +30, destructive +55, other +15) + environment (production +20, unknown +15) + decision (warn +5, confirm +15, deny +25).
