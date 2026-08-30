# Grafana dashboard

JSON: [`cliproxy.json`](cliproxy.json). UID `cliproxy-quota`. Title **CLIProxyAPI / CPA Prometheus**.

Regenerate:

```bash
python3 grafana/build.py
```

## Import

1. Grafana → Dashboards → New → Import → upload `cliproxy.json` (or paste).
2. Pick a Prometheus-compatible datasource. The dashboard variable defaults to uid `mimir` so a Grafana that already has that uid (cddlabs) just works.
3. Leave Provider and Email on **All**. Multi-select uses `${provider:regex}` / `${email:regex}` with `allValue=.*`.

Scrape must already be in Prometheus:

```
GET /v0/resource/plugins/cpa-prometheus/metrics
```

Prefix `cliproxy_*`. Every series has `plugin_id="cpa-prometheus"`.

## What it covers

Rows: Overview, Quota, Accounts, Usage.

- Quota windows (`used` / `remaining` / time-until-reset) with email in legends.
- Codex banked reset credits (`cliproxy_quota_reset_credits`).
- Credentials from `host.auth.list` (disabled, unavailable, runtime-only, project_id, last_refresh / updated_at ages).
- `usage.handle` request rate, tokens by type (excluding `total` / `cached` so they are not double-counted), latency histogram, failures by code.
- `cliproxy_model_available` stays empty unless CPA `get_runtime` includes `model_states`.

Do not scrape host `GET /metrics` (404).
