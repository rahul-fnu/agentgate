# MCP Proxy Guide

## Overview

The AgentGate MCP proxy sits between AI agents and MCP (Model Context Protocol) servers, intercepting all `tools/call` requests and evaluating them against the same YAML policy engine used for CLI shims.

```
Before:  Claude Code  →  MCP Server (filesystem, postgres, etc.)
After:   Claude Code  →  AgentGate MCP Proxy  →  MCP Server
```

This gives you deterministic guardrails on what AI agents can do through MCP tools — without modifying the agent or the server.

## Quick Start

### 1. Build AgentGate

```bash
go build -o agentgate ./cmd/agentgate
./agentgate init
./agentgate enforce
```

### 2. Wrap an MCP Server

Change your MCP client configuration to route through AgentGate:

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
      "command": "/path/to/agentgate",
      "args": [
        "mcp-proxy",
        "--server-name", "filesystem",
        "--",
        "npx",
        "@modelcontextprotocol/server-filesystem",
        "/tmp"
      ]
    }
  }
}
```

### 3. Add MCP Policies

Add policies to `~/.agentgate/policies.yaml`:

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
    suggestion: "Destructive SQL operations are blocked."
    match:
      mcp_server: ["postgres"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["DROP", "TRUNCATE", "DELETE FROM"]

  # Rate-limit database writes
  - name: rate-limit-mcp-db-writes
    priority: 95
    decision: deny
    suggestion: "Too many database write operations."
    match:
      mcp_server: ["postgres"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["INSERT", "UPDATE", "DELETE"]
    rate_limit:
      limit: 10
      window: 5m

  # Warn on any write operation through MCP
  - name: warn-mcp-writes
    priority: 50
    decision: warn
    suggestion: "MCP write operation detected."
    match:
      action_type: [write, destructive]
```

## Command Line Usage

```bash
# Basic usage
agentgate mcp-proxy -- <server-command> [server-args...]

# With explicit server name (for policy matching)
agentgate mcp-proxy --server-name filesystem -- npx @modelcontextprotocol/server-filesystem /tmp

# Server name auto-detection
# AgentGate tries to detect the server name from:
# 1. @modelcontextprotocol/server-NAME packages → "server-NAME"
# 2. Arguments containing "mcp-" → uses that argument
# 3. Falls back to the command basename
```

## How It Works

### Message Flow

The proxy operates on newline-delimited JSON-RPC 2.0 messages over stdin/stdout:

1. **Agent → Proxy**: All messages from the agent's stdout are read line-by-line
2. **tools/call interception**: If the message is a `tools/call` request, it's evaluated against policies
3. **If blocked**: A `{"isError": true}` response is sent directly back to the agent; the real server never sees the request
4. **If allowed**: The message is forwarded to the real server's stdin
5. **Server → Agent**: All responses from the server are forwarded back to the agent

### Blocked Response Format

When a tool call is blocked, the agent receives:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "AGENTGATE BLOCKED: Policy 'block-mcp-file-delete' denied this action. Risk: 90. File deletion through MCP is blocked."
      }
    ],
    "isError": true
  }
}
```

The agent sees this as a tool error and can adjust its behavior accordingly.

### Policy Matching for MCP

MCP tool calls are mapped to the policy engine as follows:

| MCP Concept | Policy Field | Example |
|-------------|-------------|---------|
| Server name | `mcp_server` or `tool` | `["filesystem"]` |
| Tool name | `mcp_tool` or `action` | `["delete_file"]` |
| Arguments | `mcp_args_contain` | `["DROP", "/etc"]` |
| Tool classification | `action_type` | `["destructive"]` |

Tool names are automatically classified:
- **read**: tools containing `read`, `get`, `list`, `describe`, `search`, `find`
- **write**: tools containing `write`, `create`, `update`, `insert`, `put`, `set`, `execute`, `run`
- **destructive**: tools containing `delete`, `drop`, `remove`, `truncate`, `destroy`, `purge`
- **other**: everything else

### Observe vs Enforce

- **Observe mode** (default): Policy decisions are logged but not enforced. Blocked calls are forwarded with a stderr warning.
- **Enforce mode**: Blocked calls are actually denied. The agent receives an error response.

```bash
./agentgate observe   # safe testing
./agentgate enforce   # real enforcement
```

## Logging

All MCP tool calls are logged to `~/.agentgate/events.jsonl` with `"source": "mcp"`:

```json
{
  "ts": "2025-01-15T10:30:00Z",
  "id": "ag_abc123",
  "phase": "start",
  "mode": "enforce",
  "tool": "filesystem",
  "cmd": "mcp:filesystem/delete_file",
  "decision": "deny",
  "policy": "block-mcp-file-delete",
  "risk": 90,
  "parse": "parsed",
  "source": "mcp"
}
```

View MCP events:

```bash
agentgate tail --tool filesystem
agentgate report --last 24h
agentgate metrics --last 24h --format json
```

## Stderr Output

The proxy produces structured stderr output for debugging:

```
AGENTGATE_MCP_PROXY=true
AGENTGATE_MCP_SESSION=ag_session_abc123
AGENTGATE_MCP_SERVER=filesystem
```

When a call is blocked:
```
AGENTGATE_MCP_BLOCKED: tool=delete_file policy=block-mcp-file-delete risk=90
```

When in observe mode:
```
AGENTGATE_MCP_OBSERVE: tool=delete_file decision=deny (simulated)
```

## Example: Securing a Database MCP Server

```yaml
policies:
  # Block all DDL operations
  - name: no-ddl-through-mcp
    priority: 100
    decision: deny
    suggestion: "Schema changes through MCP are not allowed."
    match:
      mcp_server: ["postgres", "mysql"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["CREATE TABLE", "ALTER TABLE", "DROP TABLE", "CREATE INDEX"]

  # Block mass deletes
  - name: no-mass-delete
    priority: 99
    decision: deny
    suggestion: "Mass DELETE operations are blocked."
    match:
      mcp_server: ["postgres"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["DELETE FROM"]

  # Allow SELECT queries
  - name: allow-select
    priority: 80
    decision: allow
    match:
      mcp_server: ["postgres"]
      mcp_tool: ["execute_query"]
      mcp_args_contain: ["SELECT"]
```

## Limitations

- **No request modification**: The proxy cannot modify tool call arguments — it can only allow or block.
- **No response inspection**: Responses from the server are forwarded without policy evaluation.
- **Confirm falls through to allow**: Since MCP is non-interactive, `confirm` decisions are treated as `allow` in enforce mode.
- **Environment detection**: MCP calls don't have environment detection (no kubeconfig/workspace context), so environment-based policies won't match unless the field is omitted.
