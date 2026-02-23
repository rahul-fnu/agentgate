#!/usr/bin/env bash
#
# AgentGate Simulation Test
#
# Installs kubectl, docker CLI, node/npx, and an MCP filesystem server.
# Exercises policies for kubectl, docker, git, and MCP tool calls.
# Runs live shim intercepts and a real MCP proxy integration test.
#
set -euo pipefail

export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/usr/sbin:/sbin:$PATH"

RED='\033[0;31m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

PASS=0
FAIL=0
TOTAL=0
AG="$(pwd)/agentgate"

banner() { echo -e "\n${CYAN}${BOLD}═══ $1 ═══${RESET}\n"; }
section() { echo -e "\n${BOLD}--- $1 ---${RESET}"; }

# check_decision "label" expected_decision tool -- args...
check_decision() {
    local label="$1"; shift
    local expected="$1"; shift
    TOTAL=$((TOTAL + 1))

    local output
    output=$("$AG" explain "$@" 2>/dev/null) || true
    local decision
    decision=$(echo "$output" | python3 -c "import sys,json;print(json.load(sys.stdin)['decision'])" 2>/dev/null) || decision="ERROR"

    if [ "$decision" = "$expected" ]; then
        echo -e "  ${GREEN}PASS${RESET}  $label  →  $decision"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${RESET}  $label  →  $decision (expected $expected)"
        FAIL=$((FAIL + 1))
    fi
}

# check_exit "label" expected_exit_code command args...
check_exit() {
    local label="$1"; shift
    local expected="$1"; shift
    TOTAL=$((TOTAL + 1))

    local rc=0
    "$@" >/dev/null 2>/dev/null || rc=$?

    if [ "$rc" -eq "$expected" ]; then
        echo -e "  ${GREEN}PASS${RESET}  $label  →  exit $rc"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${RESET}  $label  →  exit $rc (expected $expected)"
        FAIL=$((FAIL + 1))
    fi
}

# check_contains "label" expected_substring actual_string
check_contains() {
    local label="$1"; shift
    local expected="$1"; shift
    local actual="$1"; shift
    TOTAL=$((TOTAL + 1))

    if echo "$actual" | grep -q "$expected"; then
        echo -e "  ${GREEN}PASS${RESET}  $label"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${RESET}  $label  (expected to contain '$expected')"
        FAIL=$((FAIL + 1))
    fi
}

set_kube_context() {
    local ctx="$1"
    cat > "$HOME/.kube/config" <<EOF
apiVersion: v1
kind: Config
current-context: $ctx
contexts:
- context: {cluster: c, user: u}
  name: $ctx
clusters:
- cluster: {server: "https://fake:6443"}
  name: c
users:
- name: u
  user: {token: fake}
EOF
    rm -f "$HOME/.agentgate/cache.json"
}

########################################################################
banner "STEP 1: Install dependencies"
########################################################################

section "kubectl"
if ! command -v kubectl &>/dev/null; then
    curl -fsSL "https://dl.k8s.io/release/$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" -o /tmp/kubectl
    chmod +x /tmp/kubectl
    sudo mv /tmp/kubectl /usr/local/bin/kubectl
fi
kubectl version --client 2>/dev/null | grep -o 'v[0-9.]*' || true

section "docker CLI"
if ! command -v docker &>/dev/null; then
    curl -fsSL "https://download.docker.com/linux/static/stable/x86_64/docker-27.4.1.tgz" -o /tmp/docker.tgz
    tar -xzf /tmp/docker.tgz -C /tmp docker/docker
    sudo mv /tmp/docker/docker /usr/local/bin/docker
    rm -rf /tmp/docker /tmp/docker.tgz
fi
docker --version

section "Node.js + npx"
if ! command -v node &>/dev/null; then
    curl -fsSL https://nodejs.org/dist/v20.11.1/node-v20.11.1-linux-x64.tar.xz -o /tmp/node.tar.xz
    sudo tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1
    rm /tmp/node.tar.xz
fi
echo "node $(node --version), npx $(npx --version 2>/dev/null)"

section "MCP filesystem server"
npm list -g @modelcontextprotocol/server-filesystem &>/dev/null \
    || sudo npm install -g @modelcontextprotocol/server-filesystem 2>/dev/null
echo "installed"

########################################################################
banner "STEP 2: Build & init AgentGate"
########################################################################

cd /home/claude/agentgate
go build -o agentgate ./cmd/agentgate

# Clean state from previous runs
rm -rf "$HOME/.agentgate"

"$AG" init
"$AG" enforce

# Set unknown_env_default_decision to allow so git/docker read ops
# aren't auto-warned (we want to test specific policies, not the catch-all)
cat > "$HOME/.agentgate/config.yaml" <<'YAML'
mode: enforce
unknown_env_default_decision: allow
YAML

export PATH="$HOME/.agentgate/bin:$PATH"
echo ""
"$AG" status

########################################################################
banner "STEP 3: Set up environments"
########################################################################

mkdir -p "$HOME/.kube"
set_kube_context "production-cluster"
echo "kubeconfig: production-cluster"

########################################################################
banner "STEP 4: kubectl — production"
########################################################################

section "DENY — namespace delete, force delete, wildcard delete, drain"

check_decision "delete namespace prod" \
    deny kubectl -- delete namespace prod

check_decision "delete pod --force --grace-period=0" \
    deny kubectl -- delete pod nginx --force --grace-period=0

check_decision "delete deployments --all" \
    deny kubectl -- delete deployments --all

check_decision "drain node-1" \
    deny kubectl -- drain node-1

section "CONFIRM — generic destructive in prod"

check_decision "delete deployment nginx" \
    confirm kubectl -- delete deployment nginx

check_decision "scale --replicas=0" \
    confirm kubectl -- scale deployment/web --replicas=0

section "WARN — write operations in prod"

check_decision "apply -f manifest.yaml" \
    warn kubectl -- apply -f manifest.yaml

check_decision "create configmap" \
    warn kubectl -- create configmap my-config --from-literal=key=val

section "ALLOW — read-only"

check_decision "get pods" \
    allow kubectl -- get pods

check_decision "describe svc frontend" \
    allow kubectl -- describe svc frontend

check_decision "logs nginx" \
    allow kubectl -- logs nginx

########################################################################
banner "STEP 5: kubectl — dev environment"
########################################################################

set_kube_context "dev-sandbox"

section "ALLOW — everything is fine in dev"

check_decision "delete namespace test (dev)" \
    allow kubectl -- delete namespace test

check_decision "delete pod --force (dev)" \
    allow kubectl -- delete pod nginx --force --grace-period=0

check_decision "drain node (dev)" \
    allow kubectl -- drain node-1

check_decision "apply -f manifest (dev)" \
    allow kubectl -- apply -f manifest.yaml

########################################################################
banner "STEP 6: kubectl — unknown environment"
########################################################################

set_kube_context "my-custom-cluster"

section "CONFIRM — destructive in unknown env"

check_decision "delete pod (unknown)" \
    confirm kubectl -- delete pod nginx

check_decision "delete svc (unknown)" \
    confirm kubectl -- delete svc backend

section "ALLOW — reads still fine"

check_decision "get pods (unknown)" \
    allow kubectl -- get pods

########################################################################
banner "STEP 7: git"
########################################################################

section "DENY — force push to protected branches"

check_decision "push --force origin main" \
    deny git -- push --force origin main

check_decision "push --force origin master" \
    deny git -- push --force origin master

check_decision "push --force origin production" \
    deny git -- push --force origin production

check_decision "push --force-with-lease origin release" \
    deny git -- push --force-with-lease origin release

check_decision "reset --hard main" \
    deny git -- reset --hard main

check_decision "reset --hard master" \
    deny git -- reset --hard master

section "CONFIRM — force push to feature branches, clean -f"

check_decision "push --force origin feature-x" \
    confirm git -- push --force origin feature-x

check_decision "clean -f -d" \
    confirm git -- clean -f -d

section "ALLOW — normal git ops"

check_decision "push origin main (no force)" \
    allow git -- push origin main

check_decision "commit -m test" \
    allow git -- commit -m "test message"

check_decision "log --oneline" \
    allow git -- log --oneline

check_decision "status" \
    allow git -- status

########################################################################
banner "STEP 8: docker"
########################################################################

section "DENY — system prune with -a or --volumes"

check_decision "system prune --all --volumes" \
    deny docker -- system prune --all --volumes

check_decision "system prune -a" \
    deny docker -- system prune -a

section "CONFIRM — compose down --volumes, rm -f"

check_decision "compose down --volumes" \
    confirm docker -- compose down --volumes

check_decision "rm -f container" \
    confirm docker -- rm -f my-container

section "ALLOW — read and build ops"

check_decision "ps" \
    allow docker -- ps

check_decision "images" \
    allow docker -- images

check_decision "build ." \
    allow docker -- build .

check_decision "run nginx" \
    allow docker -- run nginx

########################################################################
banner "STEP 9: Live shim intercept (enforce mode)"
########################################################################

set_kube_context "production-cluster"

section "Blocked commands exit 77"

check_exit "kubectl delete ns prod → 77" 77 \
    "$HOME/.agentgate/bin/kubectl" delete namespace prod

check_exit "kubectl drain node-1 → 77" 77 \
    "$HOME/.agentgate/bin/kubectl" drain node-1

section "Allowed commands do NOT exit 77"

# kubectl get pods will fail to connect (exit 1), but should NOT be 77
TOTAL=$((TOTAL + 1))
rc=0
"$HOME/.agentgate/bin/kubectl" get pods --request-timeout=1s 2>/dev/null || rc=$?
if [ "$rc" -ne 77 ]; then
    echo -e "  ${GREEN}PASS${RESET}  kubectl get pods → exit $rc (not 77, not policy-blocked)"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  kubectl get pods → exit 77 (policy-blocked!)"
    FAIL=$((FAIL + 1))
fi

section "Observe mode: nothing blocked"

"$AG" observe >/dev/null
rm -f "$HOME/.agentgate/cache.json"

TOTAL=$((TOTAL + 1))
rc=0
"$HOME/.agentgate/bin/kubectl" delete namespace prod 2>/dev/null || rc=$?
if [ "$rc" -ne 77 ]; then
    echo -e "  ${GREEN}PASS${RESET}  observe: kubectl delete ns prod → exit $rc (not blocked)"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  observe: kubectl delete ns prod → exit 77 (blocked!)"
    FAIL=$((FAIL + 1))
fi

"$AG" enforce >/dev/null

########################################################################
banner "STEP 10: MCP proxy — filesystem server"
########################################################################

# Append MCP policies to existing policies.yaml
cat >> "$HOME/.agentgate/policies.yaml" <<'YAML'

  # --- MCP policies (appended for test) ---
  - name: block-mcp-file-delete
    priority: 100
    decision: deny
    suggestion: "File deletion through MCP is blocked."
    match:
      tool: [server-filesystem]
      action: [delete_file, remove_file]

  - name: block-mcp-write-etc
    priority: 100
    decision: deny
    suggestion: "Writing to /etc is blocked."
    match:
      tool: [server-filesystem]
      action: [write_file]
      raw_contains: ["/etc"]

  - name: allow-mcp-reads
    priority: 50
    decision: allow
    match:
      tool: [server-filesystem]
      action: [read_file, list_directory, get_file_info]
YAML

# Clean any stale output from previous runs
rm -f /tmp/mcp_proxy_output.jsonl /tmp/mcp_proxy_stderr.log

# Create test directory with files
TEST_DIR=$(mktemp -d)
echo "Hello from AgentGate test" > "$TEST_DIR/test.txt"
echo "Secret data" > "$TEST_DIR/secret.txt"
mkdir -p "$TEST_DIR/subdir"
echo "Nested file" > "$TEST_DIR/subdir/nested.txt"

section "Starting MCP filesystem server through agentgate proxy"

# Start the proxy in background, talking to the real filesystem MCP server
"$AG" mcp-proxy --server-name server-filesystem -- \
    npx @modelcontextprotocol/server-filesystem "$TEST_DIR" \
    < <(
        # Wait for server to be ready, then send initialize + our test messages
        sleep 2

        # 1. Initialize
        echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
        sleep 1

        # 2. Read a file (should be ALLOWED)
        echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"'"$TEST_DIR/test.txt"'"}}}'
        sleep 1

        # 3. List directory (should be ALLOWED)
        echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_directory","arguments":{"path":"'"$TEST_DIR"'"}}}'
        sleep 1

        # 4. Delete file (should be BLOCKED by policy)
        echo '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"'"$TEST_DIR/secret.txt"'"}}}'
        sleep 1

        # 5. Write to /etc (should be BLOCKED by policy)
        echo '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"write_file","arguments":{"path":"/etc/evil.conf","content":"hacked"}}}'
        sleep 1
    ) > /tmp/mcp_proxy_output.jsonl 2>/tmp/mcp_proxy_stderr.log &

MCP_PID=$!

# Wait for all messages to flow
sleep 10

# Kill the proxy
kill "$MCP_PID" 2>/dev/null || true
wait "$MCP_PID" 2>/dev/null || true

section "MCP proxy results"

echo "Raw responses:"
while IFS= read -r line; do
    if echo "$line" | python3 -c "import sys,json;json.load(sys.stdin)" 2>/dev/null; then
        echo "  $line" | python3 -c "
import sys, json
line = sys.stdin.read().strip()
try:
    msg = json.loads(line)
    mid = msg.get('id', '?')
    if 'result' in msg:
        r = msg['result']
        if isinstance(r, dict) and r.get('isError'):
            text = r.get('content', [{}])[0].get('text', '')
            print(f'  id={mid}: BLOCKED — {text[:80]}')
        elif isinstance(r, dict) and 'content' in r:
            c = r['content']
            if isinstance(c, list) and len(c) > 0:
                text = c[0].get('text', str(c[0]))[:80]
                print(f'  id={mid}: OK — {text}')
            else:
                print(f'  id={mid}: OK — {str(r)[:80]}')
        else:
            print(f'  id={mid}: OK — {str(r)[:80]}')
except:
    pass
" 2>/dev/null
    fi
done < /tmp/mcp_proxy_output.jsonl

echo ""
echo "Stderr signals:"
grep -E "AGENTGATE_MCP" /tmp/mcp_proxy_stderr.log 2>/dev/null || echo "  (none)"

# Helper to check MCP responses by id
check_mcp_response() {
    local label="$1"
    local target_id="$2"
    local expect_blocked="$3"  # "true" or "false"
    TOTAL=$((TOTAL + 1))

    local found
    found=$(python3 -c "
import json, sys
result = 'not_found'
with open('/tmp/mcp_proxy_output.jsonl') as f:
    for line in f:
        line = line.strip()
        if not line: continue
        try:
            msg = json.loads(line)
            mid = msg.get('id')
            if mid is not None and str(mid) == '$target_id' and 'result' in msg:
                r = msg['result']
                if isinstance(r, dict) and r.get('isError', False):
                    text = r.get('content', [{}])[0].get('text', '')
                    if 'BLOCKED' in text:
                        result = 'blocked'
                        break
                result = 'allowed'
                break
        except Exception: pass
print(result)
" 2>/dev/null)

    if [ "$expect_blocked" = "true" ]; then
        if [ "$found" = "blocked" ]; then
            echo -e "  ${GREEN}PASS${RESET}  $label"
            PASS=$((PASS + 1))
        else
            echo -e "  ${RED}FAIL${RESET}  $label  (got: $found)"
            FAIL=$((FAIL + 1))
        fi
    else
        if [ "$found" = "allowed" ]; then
            echo -e "  ${GREEN}PASS${RESET}  $label"
            PASS=$((PASS + 1))
        else
            echo -e "  ${RED}FAIL${RESET}  $label  (got: $found)"
            FAIL=$((FAIL + 1))
        fi
    fi
}

check_mcp_response "read_file (id=2) allowed through proxy" "2" "false"
check_mcp_response "delete_file (id=4) BLOCKED by policy" "4" "true"
check_mcp_response "write_file /etc (id=5) BLOCKED by policy" "5" "true"

# Verify: the original file still exists (wasn't actually deleted)
TOTAL=$((TOTAL + 1))
if [ -f "$TEST_DIR/secret.txt" ]; then
    echo -e "  ${GREEN}PASS${RESET}  secret.txt still exists (delete was truly blocked)"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  secret.txt was deleted despite policy!"
    FAIL=$((FAIL + 1))
fi

########################################################################
banner "STEP 11: mcp-wrap / mcp-unwrap"
########################################################################

section "Setup: create fake Claude settings with MCP servers"
mkdir -p "$HOME/.claude"
cat > "$HOME/.claude/settings.json" <<'JSON'
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["@modelcontextprotocol/server-filesystem", "/tmp"]
    },
    "postgres": {
      "command": "npx",
      "args": ["@modelcontextprotocol/server-postgres", "postgresql://localhost/db"]
    }
  },
  "permissions": {
    "allow": ["Read"]
  }
}
JSON

section "Dry run"
"$AG" mcp-wrap --dry-run
echo ""

TOTAL=$((TOTAL + 1))
cmd=$(python3 -c "import json;print(json.load(open('$HOME/.claude/settings.json'))['mcpServers']['filesystem']['command'])")
if [ "$cmd" = "npx" ]; then
    echo -e "  ${GREEN}PASS${RESET}  dry-run: settings unchanged"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  dry-run modified settings"
    FAIL=$((FAIL + 1))
fi

section "Wrap"
"$AG" mcp-wrap
echo ""

TOTAL=$((TOTAL + 1))
result=$(python3 -c "
import json
d = json.load(open('$HOME/.claude/settings.json'))
fs = d['mcpServers']['filesystem']
pg = d['mcpServers']['postgres']
ok = (
    fs['args'][0] == 'mcp-proxy'
    and pg['args'][0] == 'mcp-proxy'
    and d['permissions']['allow'] == ['Read']
)
print('ok' if ok else 'fail')
")
if [ "$result" = "ok" ]; then
    echo -e "  ${GREEN}PASS${RESET}  both servers wrapped, other settings preserved"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  wrapping failed"
    FAIL=$((FAIL + 1))
fi

section "Idempotent re-wrap"
output=$("$AG" mcp-wrap 2>&1)
echo "$output"
TOTAL=$((TOTAL + 1))
if echo "$output" | grep -q "already wrapped"; then
    echo -e "  ${GREEN}PASS${RESET}  idempotent: detected already wrapped"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  re-wrap didn't detect already wrapped"
    FAIL=$((FAIL + 1))
fi

section "Unwrap"
"$AG" mcp-unwrap
echo ""

TOTAL=$((TOTAL + 1))
result=$(python3 -c "
import json, os
d = json.load(open('$HOME/.claude/settings.json'))
fs = d['mcpServers']['filesystem']
pg = d['mcpServers']['postgres']
ok = (
    fs['command'] == 'npx'
    and fs['args'] == ['@modelcontextprotocol/server-filesystem', '/tmp']
    and pg['command'] == 'npx'
    and pg['args'] == ['@modelcontextprotocol/server-postgres', 'postgresql://localhost/db']
    and d['permissions']['allow'] == ['Read']
    and not os.path.exists('$HOME/.agentgate/mcp_originals.json')
)
print('ok' if ok else 'fail')
")
if [ "$result" = "ok" ]; then
    echo -e "  ${GREEN}PASS${RESET}  unwrap restored originals, backup removed, settings preserved"
    PASS=$((PASS + 1))
else
    echo -e "  ${RED}FAIL${RESET}  unwrap failed"
    FAIL=$((FAIL + 1))
fi

########################################################################
banner "STEP 12: Event log + report"
########################################################################

echo "Recent events:"
"$AG" tail --follow=false 2>/dev/null | python3 -c "
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line: continue
    try:
        ev = json.loads(line)
        if ev.get('phase') == 'start':
            src = ev.get('source', 'cli')
            print(f\"  [{src:3s}] {ev.get('tool','?'):18s} {ev.get('decision','?'):8s} risk={ev.get('risk','?'):>3}  policy={ev.get('policy','(default)'):35s}\")
    except: pass
" 2>/dev/null || echo "  (none)"

echo ""
"$AG" report --last 1h 2>/dev/null || true

########################################################################
banner "RESULTS"
########################################################################

# Clean up
rm -rf "$TEST_DIR" /tmp/mcp_proxy_output.jsonl /tmp/mcp_proxy_stderr.log

echo -e "  Total:  ${BOLD}$TOTAL${RESET}"
echo -e "  Passed: ${GREEN}${BOLD}$PASS${RESET}"
echo -e "  Failed: ${RED}${BOLD}$FAIL${RESET}"
echo ""

if [ "$FAIL" -eq 0 ]; then
    echo -e "${GREEN}${BOLD}ALL $TOTAL TESTS PASSED${RESET}"
    exit 0
else
    echo -e "${RED}${BOLD}$FAIL / $TOTAL TEST(S) FAILED${RESET}"
    exit 1
fi
