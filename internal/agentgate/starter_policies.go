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

  - name: no-prod-force-delete
    priority: 99
    decision: deny
    suggestion: "Force-delete patterns are blocked in production."
    match:
      tool: [kubectl]
      environment: [production]
      action: [delete]
      flags: ["force", "grace-period=0", "--force", "--grace-period=0"]

  - name: no-prod-terraform-auto-approve
    priority: 100
    decision: deny
    suggestion: "Run terraform apply with manual approval in production."
    match:
      tool: [terraform]
      environment: [production]
      action: [apply]
      flags: ["auto-approve", "--auto-approve"]

  - name: no-prod-terraform-destroy
    priority: 100
    decision: deny
    suggestion: "terraform destroy is blocked in production."
    match:
      tool: [terraform]
      environment: [production]
      action: [destroy]

  - name: no-prod-terraform-apply-destroy-flag
    priority: 99
    decision: deny
    suggestion: "terraform apply with destroy intent is blocked in production."
    match:
      tool: [terraform]
      environment: [production]
      action: [apply]
      flags: ["destroy", "--destroy"]

  - name: confirm-prod-terraform-state-mutation
    priority: 92
    decision: confirm
    suggestion: "Terraform state mutations require explicit confirmation in production."
    match:
      tool: [terraform]
      environment: [production]
      action: ["state-rm", "state-mv"]

  - name: no-prod-wildcard-delete
    priority: 98
    decision: deny
    suggestion: "Avoid wildcard deletes in production."
    match:
      tool: [kubectl]
      environment: [production]
      action: [delete]
      flags: ["all", "--all"]

  - name: no-prod-node-drain
    priority: 97
    decision: deny
    suggestion: "Node drain is blocked by default in production."
    match:
      tool: [kubectl]
      environment: [production]
      action: [drain]

  - name: no-prod-helm-uninstall
    priority: 98
    decision: deny
    suggestion: "Helm uninstall is blocked in production."
    match:
      tool: [helm]
      environment: [production]
      action: [uninstall]

  - name: no-prod-aws-ec2-terminate
    priority: 99
    decision: deny
    suggestion: "EC2 termination is blocked in production."
    match:
      tool: [aws]
      environment: [production]
      resource: [ec2]
      action: [terminate-instances]

  - name: no-prod-aws-rds-delete
    priority: 99
    decision: deny
    suggestion: "RDS delete operations are blocked in production."
    match:
      tool: [aws]
      environment: [production]
      resource: [rds]
      action: [delete-db-instance, delete-db-cluster]

  - name: no-prod-aws-s3-recursive-rm
    priority: 98
    decision: deny
    suggestion: "Recursive S3 deletes are blocked in production."
    match:
      tool: [aws]
      environment: [production]
      resource: [s3]
      action: [rm]
      flags: ["recursive", "--recursive"]

  - name: no-prod-gcloud-project-delete
    priority: 100
    decision: deny
    suggestion: "Deleting GCP projects is blocked in production."
    match:
      tool: [gcloud]
      environment: [production]
      resource: [projects, resource-manager/projects]
      action: [delete]

  - name: no-prod-gcloud-cluster-delete
    priority: 99
    decision: deny
    suggestion: "Deleting GKE clusters is blocked in production."
    match:
      tool: [gcloud]
      environment: [production]
      resource: [container/clusters]
      action: [delete]

  - name: confirm-prod-gcloud-compute-delete
    priority: 91
    decision: confirm
    suggestion: "Deleting Compute Engine instances requires confirmation in production."
    match:
      tool: [gcloud]
      environment: [production]
      resource: [compute/instances]
      action: [delete]

  - name: deny-prod-destructive-quiet-mode
    priority: 94
    decision: deny
    suggestion: "Destructive operations with quiet/non-interactive flags are blocked in production."
    match:
      environment: [production]
      action_type: [destructive]
      flags: ["quiet", "yes", "force"]

  - name: confirm-unknown-destructive
    priority: 88
    decision: confirm
    suggestion: "Environment is unknown; destructive actions require confirmation."
    match:
      environment: [unknown]
      action_type: [destructive]

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
      environment: [production, staging]
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
