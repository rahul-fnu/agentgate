# AgentGate Product Strategy (Investor-Facing)

## Vision

AgentGate becomes the safety control plane for autonomous software operations:

- Before command execution: predict risk and enforce policy.
- During execution: gate destructive operations with deterministic controls.
- After execution: produce auditable evidence and metrics.

## What is already implemented in this branch

1. Broader safety coverage:
   - Infra tools: `kubectl`, `terraform`, `helm`, `aws`, `gcloud`
   - Developer/local tools: `git`, `docker`
2. Preflight simulation:
   - `agentgate explain <tool> -- <args...>`
   - Returns environment detection, policy match, risk, and effective decision.
3. Higher precision policy model:
   - `resource_name` matching added for branch/resource-level rules.
4. Default local-risk policies:
   - force-push controls
   - hard-reset controls
   - docker prune/volume teardown controls

This is a usable product increment, not only a strategy document.

## Why this can stand out

- Most guardrails focus only on cloud infra APIs; AgentGate also protects local destructive paths where AI agents often operate.
- Uniform policy model across infra + local tooling reduces fragmentation.
- Explainability output (`agentgate explain`) improves trust and debugging for human + AI workflows.
- Local-first architecture lowers adoption friction for open-source/self-host users.

## Monetization path (open core)

Open-source/self-host (free):

- local policy engine
- CLI guardrails
- JSONL logs + basic metrics

Commercial cloud/on-prem add-ons:

- centrally managed signed policy bundles
- org-wide policy distribution + drift detection
- identity-aware approvals (teams, on-call, break-glass)
- managed analytics, incident timelines, and attack/runaway detection
- compliance exports and tamper-evident audit retention

## GTM wedge

- Start with AI-heavy infra/dev teams using coding agents.
- Position around preventing high-cost outages and destructive local mistakes.
- Offer "drop-in safety in <10 min" with clear before/after incident metrics.

## Metrics to prove product value

- blocked destructive commands per week
- near-miss rate (confirm + deny counts)
- mean time to safe resolution for blocked actions
- policy coverage growth (% commands parsed vs unknown)
- false-positive override rate

## 90-day roadmap

1. Policy UX and safety depth:
   - richer rule conditions (time windows, branch patterns, actor tags)
   - policy test command with fixtures
2. Integrations:
   - Prometheus/Grafana starter pack
   - GitHub Actions and CI wrappers
3. Enterprise controls:
   - signed policies
   - role-aware remote approvals
   - central multi-workspace policy sync
