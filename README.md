# AgentGate V1

AgentGate is a local policy-enforcement shim for infra and local-ops CLIs (`kubectl`, `terraform`, `helm`, `aws`, `gcloud`, `git`, `docker`).

It wraps your CLI calls, evaluates YAML policies, and decides to:

- `allow`
- `warn`
- `confirm`
- `deny` (exit code `77`)

No server. No DB. V1 is file-based and local.

## Quick Start (for your friend)

### 1. Clone and build

```bash
git clone https://github.com/rahul-fnu/agentgate.git
cd agentgate

# if go is not installed:
# brew install go

go mod tidy
gofmt -w ./cmd ./internal
go build -o agentgate ./cmd/agentgate
```

### 2. Initialize shims

```bash
./agentgate init
export PATH="$HOME/.agentgate/bin:$PATH"
rehash 2>/dev/null || true
hash -r 2>/dev/null || true
```

Check path order:

```bash
type -a kubectl
```

Expected: `~/.agentgate/bin/kubectl` appears before system kubectl.

### 3. Switch modes

```bash
./agentgate observe   # start safe: simulate blocking decisions
./agentgate enforce   # actually block/confirm when ready
```

## Basic Commands

- `agentgate init`
- `agentgate status`
- `agentgate observe`
- `agentgate enforce`
- `agentgate tail --env production --tool kubectl --decision deny`
- `agentgate report --last 7d`
- `agentgate explain <tool> -- <args...>`
- `agentgate allow-once <command-id>`
- `agentgate uninstall`

Hidden command used by shims:

- `agentgate __intercept <tool> -- <args...>`

## Standout Product Features (This Branch)

- Expanded command surface to include `git` and `docker` safety controls.
- New `agentgate explain` preflight simulator for "what would happen if this runs?"
- New policy matching field `resource_name` for precise branch/resource-level controls.
- Starter policy pack now includes developer workstation blast-radius guards:
  - force-push protections
  - hard-reset protections
  - docker prune/compose teardown controls

## Safe Local Test (No Real Cluster Needed)

Create a fake kubeconfig that looks like production:

```bash
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
```

Then test policy behavior:

```bash
# enforce mode so blocks are real
./agentgate enforce

# expected: deny (exit 77) from no-prod-namespace-delete
KUBECONFIG=/tmp/ag-kubeconfig kubectl delete namespace ag-test

# expected: warn for production write
KUBECONFIG=/tmp/ag-kubeconfig kubectl apply -f /tmp/does-not-matter.yaml
```

Info/usage commands are passthrough (no AgentGate output/logging), for example:

```bash
kubectl
kubectl --help
kubectl --version
kubectl -h
kubectl -v
kubectl delete
```

## Data Files (JSONL / YAML)

Under `~/.agentgate/`:

- `events.jsonl` (append-only, start/end events)
- `history.jsonl` (append-only helper history for policy checks)
- `bypasses.jsonl` (append-only issue/consume records)
- `config.yaml`
- `policies.yaml`

Events rotate at 50 MB with 3 retained files.

## Agent Stderr Contract

For intercepted commands, AgentGate prints these to stderr:

- `AGENTGATE_ACTIVE`
- `AGENTGATE_DECISION`
- `AGENTGATE_POLICY`
- `AGENTGATE_RISK`
- `AGENTGATE_ENVIRONMENT`
- `AGENTGATE_COMMAND_ID`
- `AGENTGATE_CONFIRM_REQUIRED`
- `AGENTGATE_SUGGESTION`

## Bypass (One-Time)

If a command is denied/confirm-blocked, copy `AGENTGATE_COMMAND_ID` and issue:

```bash
agentgate allow-once <command-id>
```

Token is valid for 5 minutes and 1 matching command.

## Known Limitations (V1)

- PATH shims can be bypassed using absolute binary paths or different PATH precedence.
- Parsing is best effort; unknown patterns fail open and are logged.
- Environment detection is heuristic; false positives/negatives are possible.
- No centralized org-wide enforcement in V1.
