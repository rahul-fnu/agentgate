# Testing Guide

## Running Tests

```bash
# Run all tests
make test
# or
go test ./... -v

# Run specific package tests
go test ./internal/agentgate/... -v
go test ./internal/mcp/... -v

# Run a specific test
go test ./internal/agentgate/... -run TestParseKubectl -v

# Run with race detector
go test ./... -race

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Structure

### Unit Tests (`internal/agentgate/`)

| File | Tests | What It Covers |
|------|-------|---------------|
| `parser_test.go` | 60+ cases | All 7 CLI parsers (kubectl, terraform, helm, aws, gcloud, git, docker), `classifyActionType`, `firstPositional`, `collectPositionals`, `looksLikeScaleToZero`, `looksLikeActionToken`, `consumesNextValueFlag`, `hasFlag` |
| `policy_test.go` | 15 cases | `policyMatchesContext` (all match fields including ResourceName), priority resolution, tie-breaking, rate limits, require-plan, `baseRisk`, `scoreRisk`, `restrictiveness`, unknown env defaults |
| `config_test.go` | 10 cases | `LoadConfig` (missing, valid, invalid mode, malformed YAML), `SaveConfig`, `EnsureConfig`, `LoadPolicies` (missing, starter, malformed), `EnsureDirs` |
| `bypass_test.go` | 5 cases | `IssueBypass`, `ConsumeValidBypass`, double-consume prevention, expiry, wrong hash |
| `logging_test.go` | 7 cases | `AppendEvent`, multiple events, `ReadLastLines`, `FindStartEventByID`, `StartEvent` action fields |
| `environment_test.go` | 12 cases | `classifyEnvironment` for all pattern types, unknown tool detection, git/docker (no env) |
| `evaluate_test.go` | 4 cases | `EvaluateCommand` integration (basic allow, with policies, fail-open, starter policies) |
| `metrics_test.go` | 6 cases | `CollectMetrics` (empty, with events, in-flight, filter by time), Prometheus format, JSON format |
| `util_test.go` | 8 cases | `MatchPattern` (substring, wildcard, empty, case-insensitive), `Normalize`, `ParseWindow`, `ParseLastDuration`, `CommandHash`, `BuildRawCommand`, `NewCommandID` |

### MCP Tests (`internal/mcp/`)

| File | Tests | What It Covers |
|------|-------|---------------|
| `proxy_test.go` | 10 cases | `ParseProxyArgs` (basic, with server name, equals syntax, missing separator, empty), `DeriveServerName`, `EvaluateToolCall` (allow, deny with policy, args contain), `classifyMCPAction`, `handleToolsCall` (block, allow, observe mode) |

### End-to-End Tests (`internal/agentgate/`)

| File | Tests | What It Covers |
|------|-------|---------------|
| `e2e_test.go` | 10+ cases | Full init → enforce → explain → metrics pipeline, policy loading from starter set, MCP proxy policy evaluation, event logging verification |

## Test Patterns

### Temporary Directories

All tests use `t.TempDir()` for file I/O, ensuring no shared state:

```go
func testPaths(t *testing.T) Paths {
    dir := t.TempDir()
    return Paths{
        Root:         dir,
        BinDir:       filepath.Join(dir, "bin"),
        ConfigPath:   filepath.Join(dir, "config.yaml"),
        PoliciesPath: filepath.Join(dir, "policies.yaml"),
        EventsPath:   filepath.Join(dir, "events.jsonl"),
        BypassesPath: filepath.Join(dir, "bypasses.jsonl"),
        CachePath:    filepath.Join(dir, "cache.json"),
        EventsLock:   filepath.Join(dir, ".events.lock"),
    }
}
```

### Table-Driven Tests

Parser tests use table-driven patterns for comprehensive coverage:

```go
func TestParseKubectl(t *testing.T) {
    tests := []struct {
        name       string
        args       []string
        wantAction string
        wantType   string
        // ...
    }{
        {"delete namespace", []string{"delete", "namespace", "foo"}, "delete", "destructive"},
        {"get pods", []string{"get", "pods"}, "get", "read"},
        // 10+ more cases
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            ctx := ParseCommand("kubectl", tt.args, "/tmp", false)
            // assertions
        })
    }
}
```

### Policy Integration Tests

Tests that verify the full evaluation pipeline:

```go
func TestEvaluateCommand_StarterPolicies(t *testing.T) {
    // Write starter policies to temp dir
    // Verify specific commands get expected decisions
    // e.g., git force push to main → deny
}
```

## Manual Testing

### CLI Shim Testing

```bash
# Build and init
go build -o agentgate ./cmd/agentgate
./agentgate init
export PATH="$HOME/.agentgate/bin:$PATH"
./agentgate enforce

# Test explain (no real binary needed)
./agentgate explain kubectl -- delete namespace prod
./agentgate explain terraform -- destroy
./agentgate explain git -- push --force origin main
./agentgate explain docker -- system prune --all

# Test with fake kubeconfig
cat >/tmp/ag-kubeconfig <<'EOF'
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: http://127.0.0.1:65535
  name: local
contexts:
- context:
    cluster: local
    user: local
  name: my-prod-context
current-context: my-prod-context
users:
- name: local
  user: {}
EOF

KUBECONFIG=/tmp/ag-kubeconfig kubectl delete namespace ag-test
# Expected: exit 77, deny

# Check events
./agentgate tail
./agentgate report --last 1h
./agentgate metrics --last 1h --format json
```

### MCP Proxy Testing

```bash
# Create a simple echo MCP server for testing
cat > /tmp/echo-mcp.sh <<'SCRIPT'
#!/bin/bash
while IFS= read -r line; do
  echo "$line"
done
SCRIPT
chmod +x /tmp/echo-mcp.sh

# Run proxy
./agentgate mcp-proxy --server-name test -- /tmp/echo-mcp.sh

# In another terminal, send JSON-RPC to stdin:
echo '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"/etc/passwd"}}}' | \
  ./agentgate mcp-proxy --server-name filesystem -- cat
```

## Code Quality

```bash
# Format check
make lint
# or
gofmt -l ./cmd ./internal

# Format fix
make format
# or
gofmt -w ./cmd ./internal

# Build
make build
```

## No External Dependencies

All tests run with only the Go standard library and `gopkg.in/yaml.v3`. No test frameworks, no mocking libraries, no external services required.
