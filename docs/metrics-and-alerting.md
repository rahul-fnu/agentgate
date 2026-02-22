# Metrics and Alerting Starter

## Run local metrics endpoint

```bash
agentgate serve-metrics --addr 127.0.0.1:9765 --last 24h
```

## Prometheus scrape job example

```yaml
scrape_configs:
  - job_name: "agentgate"
    static_configs:
      - targets: ["127.0.0.1:9765"]
```

## Suggested first alerts

- High block volume (possible runaway agent or broken policy rollout):
  - `sum(rate(agentgate_commands_blocked_total[5m])) > 0.2`
- Unknown environment destructive activity:
  - `sum(rate(agentgate_commands_total{environment="unknown",decision="confirm"}[10m])) > 0`
- Parse failures increasing (parser coverage gap):
  - `sum(rate(agentgate_parse_status_total{parse_status="unknown"}[10m])) > 0`

## Suggested dashboard panels

- Blocked commands by policy (bar chart)
- Decision volume by environment (stacked)
- Allowed vs blocked trend (time series)
- Parse status trend to prioritize parser improvements
