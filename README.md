# AgentGate

Deterministic policy enforcement for CLI tools and AI agent MCP connections. Open source, local-first, zero external dependencies.

AgentGate intercepts commands at two layers:

1. **CLI Shims** — PATH-based interception of `kubectl`, `terraform`, `helm`, `aws`, `gcloud`, `git`, `docker`
2. **MCP Proxy** — JSON-RPC interception between AI agents and MCP servers

Both layers evaluate commands against a shared YAML policy engine and enforce decisions: **allow**, **warn**, **confirm**, or **deny** (exit code `77`).

## Install

```bash
git clone https://github.com/rahul-fnu/agentgate.git
cd agentgate
go build -o agentgate ./cmd/agentgate
sudo mv agentgate /usr/local/bin/
```

## Quick Start

```bash
# Initialize shims and default policies
agentgate init
export PATH="$HOME/.agentgate/bin:$PATH"

# Start in observe mode (logs decisions but never blocks)
agentgate observe

# Check everything is working
agentgate status
```

## Production Setup

### 1. Enable enforcement

```bash
agentgate enforce
```

In enforce mode, denied commands exit with code `77` and are never executed. Warned commands execute with a stderr notice. Confirm commands prompt interactively (or deny if non-interactive, which is the safe default for AI agents).

### 2. Add PATH to your shell profile

```bash
# Add to ~/.bashrc, ~/.zshrc, or equivalent
export PATH="$HOME/.agentgate/bin:$PATH"
```

Verify shim precedence:

```bash
type -a kubectl
# Expected: ~/.agentgate/bin/kubectl appears first
```

### 3. Configure policies

Edit `~/.agentgate/policies.yaml`:

```yaml
policies:
  # Block namespace deletion in production
  - name: no-prod-namespace-delete
    priority: 100
    decision: deny
    suggestion: "Namespace deletion in production is blocked."
    match:
      tool: [kubectl]
      action: [delete]
      resource: [namespace, ns]
      environment: [production]

  # Require confirmation for terraform destroy
  - name: confirm-terraform-destroy
    priority: 100
    decision: confirm
    suggestion: "Terraform destroy requires confirmation."
    match:
      tool: [terraform]
      action: [destroy]

  # Block force-push to main/master
  - name: deny-force-push-protected
    priority: 100
    decision: deny
    suggestion: "Force-pushing to protected branches is blocked."
    match:
      tool: [git]
      action: [push-force]
      resource_name: [main, master]

  # Warn on all production writes
  - name: warn-prod-writes
    priority: 50
    decision: warn
    suggestion: "Write operation in production."
    match:
      environment: [production]
      action_type: [write]
```

AgentGate ships with a starter policy set covering common dangerous operations. See `agentgate init` output for the full list.

### 4. Test before enforcing

Use `explain` to simulate any command without running it:

```bash
agentgate explain kubectl -- delete namespace prod
# Output: decision=deny, policy=no-prod-namespace-delete, risk=99

agentgate explain terraform -- destroy
# Output: decision=confirm, policy=confirm-terraform-destroy

agentgate explain git -- push --force origin main
# Output: decision=deny, policy=deny-force-push-protected

agentgate explain kubectl -- get pods
# Output: decision=allow, risk=15
```

## MCP Proxy (AI Agent Guardrails)

The MCP proxy intercepts tool calls between AI agents (Claude Code, Cursor, etc.) and MCP servers, applying the same policy engine.

### Setup

Wrap any MCP server by changing the command in your agent's config:

**Before** (Claude Code `settings.json`):
```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["@modelcontextprotocol/server-filesystem", "/tmp"]
    }
  }
}
```

**After**:
```json
{
  "mcpServers": {
    "filesystem": {
      "command": "agentgate",
      "args": [
        "mcp-proxy", "--server-name", "filesystem", "--",
        "npx", "@modelcontextprotocol/server-filesystem", "/tmp"
      ]
    }
  }
}
```

### MCP Policies

Add MCP-specific policies to `~/.agentgate/policies.yaml`:

```yaml
policies:
  # Block file deletion through MCP
  - name: block-mcp-file-delete
    priority: 100
    decision: deny
    suggestion: "File deletion through MCP is blocked."
    match:
      mcp_server: ["filesystem"]
      mcp_tool: ["delete_file", "remove_file"]

  # Block destructive SQL through MCP
  - name: block-mcp-destructive-sql
    priority: 100
    decision: deny
    suggestion: "Destructive SQL via MCP is blocked."
    match:
      mcp_server: ["postgres"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["DROP", "TRUNCATE", "DELETE FROM"]

  # Warn on any MCP write operation
  - name: warn-mcp-writes
    priority: 50
    decision: warn
    suggestion: "MCP write operation detected."
    match:
      action_type: [write, destructive]
```

When a tool call is blocked, the agent receives a standard MCP error response:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [{"type": "text", "text": "AGENTGATE BLOCKED: Policy 'block-mcp-file-delete' denied this action."}],
    "isError": true
  }
}
```

The agent sees this as a tool error and adjusts its behavior. The real MCP server never receives the request.

## Monitoring

### Event log

All intercepted commands (CLI and MCP) are logged to `~/.agentgate/events.jsonl`:

```bash
# Live tail of recent events
agentgate tail

# Filter by tool, environment, or decision
agentgate tail --tool kubectl --env production --decision deny

# Summary report
agentgate report --last 24h
```

### Prometheus metrics

```bash
# One-shot output
agentgate metrics --last 24h --format prometheus
agentgate metrics --last 24h --format json

# HTTP server for scraping
agentgate serve-metrics --addr 127.0.0.1:9765 --last 24h
# GET /metrics  (Prometheus text format)
# GET /healthz
```

Exposed metrics:

| Metric | Labels |
|--------|--------|
| `agentgate_commands_total` | tool, environment, decision, mode |
| `agentgate_commands_blocked_total` | tool, environment, policy |
| `agentgate_commands_allowed_total` | tool, environment, outcome |
| `agentgate_parse_status_total` | tool, status |
| `agentgate_inflight_commands` | — |

## CLI Reference

| Command | Description |
|---------|-------------|
| `agentgate init` | Create shims, default config, and starter policies |
| `agentgate status` | Show current mode, shim status, policy count |
| `agentgate observe` | Switch to observe mode (log only, never block) |
| `agentgate enforce` | Switch to enforce mode (actually block denied commands) |
| `agentgate explain <tool> -- <args>` | Simulate a command and show the decision |
| `agentgate tail [--tool X] [--env X] [--decision X]` | View recent events |
| `agentgate report --last <duration>` | Summary of events in a time window |
| `agentgate metrics --last <duration> --format <prometheus\|json>` | Export metrics |
| `agentgate serve-metrics --addr <host:port> --last <duration>` | HTTP metrics server |
| `agentgate allow-once <command-id>` | Issue a one-time bypass token (5 min, 1 use) |
| `agentgate mcp-proxy [--server-name X] -- <cmd> <args>` | Run MCP proxy |
| `agentgate uninstall` | Remove shims |

## Policy Reference

### Match fields

| Field | CLI Source | MCP Source |
|-------|-----------|------------|
| `tool` | CLI tool name | MCP server name |
| `action` | Parsed action verb | MCP tool name |
| `action_type` | Classified: read/write/destructive/other | Classified from tool name |
| `environment` | Detected from kubeconfig/workspace/profile | N/A |
| `resource` | Parsed resource type | N/A |
| `resource_name` | Parsed resource name | N/A |
| `namespace` | `-n` / `--namespace` flag | N/A |
| `flags` | Parsed CLI flags | N/A |
| `raw_contains` | Full command string | N/A |
| `mcp_server` | N/A | MCP server name |
| `mcp_tool` | N/A | MCP tool name |
| `mcp_args_contain` | N/A | MCP argument values |

### Advanced rules

```yaml
# Rate limiting: deny if more than 5 writes in 10 minutes
- name: rate-limit-writes
  priority: 95
  decision: deny
  match:
    action_type: [write, destructive]
  rate_limit:
    limit: 5
    window: 10m

# Require terraform plan before apply
- name: require-plan-before-apply
  priority: 95
  decision: deny
  suggestion: "Run terraform plan first."
  match:
    tool: [terraform]
    action: [apply]
  require_plan:
    tool: terraform
    action: plan
    max_age: 30m
```

### Pattern matching

- Non-wildcard patterns use **substring matching**: `delete` matches `force-delete`
- Wildcard patterns use `filepath.Match` semantics: `*prod*` matches `production`
- Highest priority matching policy wins; ties break by restrictiveness (deny > confirm > warn > allow)

## Risk Scoring

Every intercepted command gets a risk score (1-99):

| Factor | Score |
|--------|-------|
| Base | +10 |
| Action: read | +5 |
| Action: write | +30 |
| Action: destructive | +55 |
| Environment: production | +20 |
| Environment: unknown | +15 |
| Decision: warn | +5 |
| Decision: confirm | +15 |
| Decision: deny | +25 |

Example: `kubectl delete ns prod` in production with deny = 10 + 55 + 20 + 25 = 99 (capped).

## Bypass

If a command is denied and you need to run it:

```bash
# Copy the AGENTGATE_COMMAND_ID from stderr output, then:
agentgate allow-once <command-id>

# Re-run the command within 5 minutes
```

Bypass tokens are single-use and expire after 5 minutes.

## Stderr Contract

Every intercepted command emits structured stderr that agents and wrappers can parse:

```
AGENTGATE_ACTIVE=true
AGENTGATE_DECISION=deny
AGENTGATE_POLICY=no-prod-namespace-delete
AGENTGATE_RISK=99
AGENTGATE_ENVIRONMENT=production
AGENTGATE_COMMAND_ID=ag_a1b2c3d4e5f6
AGENTGATE_CONFIRM_REQUIRED=false
AGENTGATE_SUGGESTION=Namespace deletion in production is blocked.
```

## Design Principles

- **Fail-open**: Config errors, parse failures, and environment detection failures all default to allowing the command. AgentGate never blocks legitimate work due to its own bugs.
- **Zero dependencies**: Only Go standard library + `gopkg.in/yaml.v3`. No databases, no servers, no network calls.
- **Local-first**: All data stored in `~/.agentgate/`. Works offline, works in CI, works in containers.
- **Deterministic**: No AI in the decision path. Policies are YAML rules evaluated by priority. Same input always produces the same decision.

## Development

```bash
make build      # go build -o agentgate ./cmd/agentgate
make test       # go test ./... -v
make lint       # gofmt -l ./cmd ./internal
make format     # gofmt -w ./cmd ./internal
```

## Architecture

See [docs/architecture.md](docs/architecture.md) for the full execution flow, directory structure, and design details.

## License

Open source. See [LICENSE](LICENSE) for details.
