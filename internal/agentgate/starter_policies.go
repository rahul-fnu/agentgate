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
      action_type: [write]

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

  - name: deny-git-force-push-protected
    priority: 99
    decision: deny
    suggestion: "Force pushes to protected branches are blocked."
    match:
      tool: [git]
      action: [push-force]
      resource_name: [main, master, production, release, trunk]

  - name: confirm-git-force-push
    priority: 90
    decision: confirm
    suggestion: "Force pushes require explicit confirmation."
    match:
      tool: [git]
      action: [push-force]

  - name: deny-git-reset-hard-protected
    priority: 96
    decision: deny
    suggestion: "Hard resets are blocked on protected branches."
    match:
      tool: [git]
      action: [reset-hard]
      raw_contains: [" main", " master", " production", " release", " trunk"]

  - name: confirm-git-clean-force
    priority: 90
    decision: confirm
    suggestion: "git clean with force can delete local work; confirmation required."
    match:
      tool: [git]
      action: [clean-force]

  - name: deny-docker-system-prune-destructive
    priority: 97
    decision: deny
    suggestion: "docker system prune with image/volume cleanup is blocked by default."
    match:
      tool: [docker]
      action: [system-prune]
      flags: ["a", "all", "volumes", "--all", "--volumes"]

  - name: confirm-docker-compose-down-volumes
    priority: 90
    decision: confirm
    suggestion: "docker compose down --volumes requires confirmation."
    match:
      tool: [docker]
      action: [compose-down]
      flags: ["volumes", "--volumes"]

  - name: confirm-docker-rm-force
    priority: 88
    decision: confirm
    suggestion: "Forced container removal requires confirmation."
    match:
      tool: [docker]
      action: [rm-force]
`
