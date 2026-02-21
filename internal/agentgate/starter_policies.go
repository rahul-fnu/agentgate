package agentgate

const starterPoliciesYAML = `policies:
  - name: no-prod-namespace-delete
    priority: 100
    decision: deny
    suggestion: "Deleting namespaces in production is blocked."
    match:
      tool: [kubectl]
      environment: [production]
      action: [delete]
      resource: [namespace, namespaces, ns]

  - name: no-prod-terraform-auto-approve
    priority: 100
    decision: deny
    suggestion: "Run terraform apply with manual approval in production."
    match:
      tool: [terraform]
      environment: [production]
      action: [apply]
      flags: ["auto-approve", "--auto-approve"]

  - name: no-prod-wildcard-delete
    priority: 98
    decision: deny
    suggestion: "Avoid wildcard deletes in production."
    match:
      tool: [kubectl]
      environment: [production]
      action: [delete]
      flags: ["all", "--all"]

  - name: confirm-prod-destructive
    priority: 90
    decision: confirm
    suggestion: "Double-check destructive operations in production."
    match:
      environment: [production]
      action_type: [destructive]

  - name: confirm-prod-scale-to-zero
    priority: 89
    decision: confirm
    suggestion: "Scaling to zero in production requires confirmation."
    match:
      tool: [kubectl]
      environment: [production]
      action: [scale]
      flags: ["replicas=0", "--replicas=0"]

  - name: warn-prod-writes
    priority: 70
    decision: warn
    suggestion: "Production write operation."
    match:
      environment: [production]
      action: [apply, patch, upgrade]

  - name: prod-write-rate-limit
    priority: 95
    decision: deny
    suggestion: "Rate limit reached for production writes (possible runaway loop)."
    match:
      environment: [production]
      action_type: [write, destructive]
    rate_limit:
      limit: 20
      window: 5m

  - name: terraform-plan-before-apply
    priority: 96
    decision: deny
    suggestion: "Run terraform plan in this directory before apply."
    match:
      tool: [terraform]
      action: [apply]
      environment: [production]
    require_plan:
      window: 2h
`
