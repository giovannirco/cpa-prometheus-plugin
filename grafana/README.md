# Grafana dashboard

JSON: [`cliproxy.json`](cliproxy.json). UID `cliproxy-quota`. Title **CLIProxyAPI / CPA Prometheus**.

Regenerate:

```bash
python3 grafana/build.py
```

## Import

1. Grafana → Dashboards → New → Import → upload `cliproxy.json` (or paste).
2. Pick a Prometheus-compatible datasource. The dashboard variable defaults to uid `mimir`; Grafana rebinds it on import if that uid is missing.
3. Leave Provider and Email on **All**. Multi-select uses `${provider:regex}` / `${email:regex}` with `allValue=.*`.

Scrape must already be in Prometheus / Mimir / Alloy:

```
GET /v0/management/plugins/cpa-prometheus/metrics
```

Scrape it with the CPA management key as `Authorization: Bearer`. Since v0.2.0 that management route is the only metrics endpoint; the old resource path (`/v0/resource/plugins/cpa-prometheus/metrics`, `public-metrics`, `scrape-token`) is gone.

Prefix `cliproxy_*`. Every series has `plugin_id="cpa-prometheus"`.

## What it covers

Rows: Overview, Quota, Accounts, Usage.

- Quota windows (`used` / `remaining` / time-until-reset) with email in legends.
- Codex banked reset credits (`cliproxy_quota_reset_credits`).
- Credentials from `host.auth.list` (disabled, unavailable, runtime-only, project_id, last_refresh / updated_at ages).
- `usage.handle` request rate, tokens by type (excluding `total` / `cached` so they are not double-counted), latency histogram, failures by code.
- `cliproxy_model_available` stays empty unless CPA `get_runtime` includes `model_states`.

Do not scrape host `GET /metrics` (404).
