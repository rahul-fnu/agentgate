# Guardrails Research: High-Risk Commands and AI Incident Reduction

## Why this matters

Agentic tools can execute destructive infra commands quickly and repeatedly. The December AWS outage reported by the Financial Times and covered by Engadget is a useful example: an internal AI tool reportedly performed a destructive environment re-creation workflow, causing a long outage window. AWS attributed the incident to permissions/user-access controls, which still reinforces the same policy lesson: least privilege plus high-friction controls on destructive paths.

- Engadget summary: [13-hour AWS outage reportedly caused by Amazon's own AI tools](https://www.engadget.com/ai/13-hour-aws-outage-reportedly-caused-by-amazons-own-ai-tools-170930190.html)

## Missing dangerous commands (and recommended V1 defaults)

The list below focuses on commands with large blast radius that were either missing or under-enforced by default.

| Tool | Command pattern | Suggested default | Why |
|---|---|---|---|
| kubectl | `delete namespace` | `deny` in prod | Immediate broad deletion blast radius. |
| kubectl | `delete --force` / `--grace-period=0` | `deny` in prod | Skips graceful termination and increases irreversible loss risk. |
| kubectl | `delete --all` | `deny` in prod | Bulk delete pattern is very high risk for automation loops. |
| kubectl | `drain <node>` | `deny` in prod | Can trigger broad workload disruption if repeated. |
| terraform | `apply -auto-approve` | `deny` in prod | Removes human gate for infrastructure mutation. |
| terraform | `destroy` | `deny` in prod | Explicit destructive intent over entire stack scope. |
| terraform | `apply --destroy` | `deny` in prod | Destructive apply mode should not be autonomous by default. |
| terraform | `state rm`, `state mv` | `confirm` in prod | State surgery can cause drift, orphaning, and recovery complexity. |
| helm | `uninstall` | `deny` in prod | Removes deployed release objects quickly. |
| aws | `ec2 terminate-instances` | `deny` in prod | Direct workload termination. |
| aws | `rds delete-db-instance`, `delete-db-cluster` | `deny` in prod | Data durability risk. |
| aws | `s3 rm --recursive` | `deny` in prod | Bulk object removal. |
| gcloud | `projects delete` | `deny` in prod | Max blast radius at project boundary. |
| gcloud | `container clusters delete` | `deny` in prod | Cluster teardown is highly disruptive. |
| gcloud | `compute instances delete` | `confirm` in prod | Significant but often narrower than project deletion. |

## AI-specific policy design choices

- Prefer `deny` for explicit teardown operations in production.
- Require `confirm` for destructive actions when environment is `unknown`.
- Block destructive actions with non-interactive override flags (`--quiet`, `--yes`, `--force`) in production.
- Keep write-rate limits to detect runaway loops quickly.
- Require `terraform plan` before `terraform apply` in sensitive environments.

## References (official command docs)

- kubectl command reference: [kubectl](https://kubernetes.io/docs/reference/generated/kubectl/kubectl-commands)
- kubectl drain: [kubectl drain](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_drain/)
- Terraform apply: [terraform apply](https://developer.hashicorp.com/terraform/cli/commands/apply)
- Terraform destroy: [terraform destroy](https://developer.hashicorp.com/terraform/cli/commands/destroy)
- Terraform state rm: [terraform state rm](https://developer.hashicorp.com/terraform/cli/commands/state/rm)
- Helm uninstall: [helm uninstall](https://helm.sh/docs/helm/helm_uninstall/)
- AWS CLI EC2 terminate: [terminate-instances](https://docs.aws.amazon.com/cli/latest/reference/ec2/terminate-instances.html)
- AWS CLI S3 rm: [s3 rm](https://docs.aws.amazon.com/cli/latest/reference/s3/rm.html)
- gcloud projects delete: [gcloud projects delete](https://cloud.google.com/sdk/gcloud/reference/projects/delete)
- gcloud compute instances delete: [gcloud compute instances delete](https://cloud.google.com/sdk/gcloud/reference/compute/instances/delete)
