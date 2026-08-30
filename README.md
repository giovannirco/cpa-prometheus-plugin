# CPA Prometheus

CLIProxyAPI plugin that exports Prometheus metrics for proxy traffic and provider quota.

CPA has no `/metrics` of its own. This plugin is an in-process `.so`. It hooks `usage.handle` for request stats, polls quota over `host.auth` + `host.http.do`, and serves text on a resource route. Host `GET /metrics` stays 404.

Needs a plugin-capable CPA v7.2.x image. The `_no-plugin` builds cannot load it.

## Scrape

CPA resource routes are **not** management-authenticated. The store maintainer wants those routes limited to static/UI content, so this plugin defaults to **closed** on the resource path.

| Path | Auth |
|------|------|
| `GET /v0/management/plugins/cpa-prometheus/metrics` | CPA management key (`Authorization: Bearer`). Always served. |
| `GET /v0/resource/plugins/cpa-prometheus/metrics` | **401 by default.** Set `public-metrics: true` for an open LAN scrape, or set `scrape-token` and send `Authorization: Bearer` / `X-Scrape-Token`. |

If `scrape-token` is set, it **wins**: resource GET is 401 unless the token is presented, even when `public-metrics` is true.

Grafana Alloy example (dedicated scrape token, not the management key):

```alloy
prometheus.scrape "cliproxyapi" {
  targets      = [{"__address__" = "cliproxyapi.cliproxyapi.svc:8317"}]
  metrics_path = "/v0/resource/plugins/cpa-prometheus/metrics"
  bearer_token = sys.env("CPA_PROMETHEUS_SCRAPE_TOKEN")
  job_name     = "cliproxyapi"
}
```

Prometheus Operator `ServiceMonitor` `bearerTokenSecret` works the same way. For an open LAN scrape, set `public-metrics: true` and leave `scrape-token` empty; a Kubernetes scrape with `prometheus.io/scrape` + `prometheus.io/path` is enough.

Token-shaped values, cookies, file paths, and raw API keys are dropped from labels. Email stays as an identifier.

## Metrics

Prefix `cliproxy_*`. Every series has `plugin_id="cpa-prometheus"`.

From each completed proxy request (`usage.handle`):

| Name | Type |
|------|------|
| `cliproxy_requests_total` | counter |
| `cliproxy_failures_total` | counter (`code` is the HTTP status when we have one) |
| `cliproxy_request_duration_seconds` | histogram |
| `cliproxy_tokens_total` | counter (`type` = input, output, reasoning, cached, cache_read, cache_creation, total) |
| `cliproxy_models_seen` | gauge of distinct models seen so far |
| `cliproxy_model_seen` | 1 per `provider`,`model` once observed |
| `cliproxy_last_request_timestamp_seconds` | unix of `usage.handle` `RequestedAt` (or now if CPA omitted it) |

CPA has no host model-list callback. `cliproxy_model_available` is emitted only when `host.auth.get_runtime` includes `model_states`.

From `host.auth.list`, refreshed on the quota tick:

| Name | Type |
|------|------|
| `cliproxy_credentials` | gauge by `provider`, `status`, `email`, `account_type` |
| `cliproxy_auth_success` | gauge, host snapshot (not a Prom counter) |
| `cliproxy_auth_failed` | gauge, same |
| `cliproxy_auth_disabled` | 0/1 |
| `cliproxy_auth_unavailable` | 0/1 |
| `cliproxy_auth_next_retry_timestamp_seconds` | unix, only while the cred is cooling down |
| `cliproxy_auth_runtime_only` | 0/1, no backing auth file |
| `cliproxy_auth_last_refresh_timestamp_seconds` | unix, only when `last_refresh` is set |
| `cliproxy_auth_updated_timestamp_seconds` | unix, only when `updated_at` is set |
| `cliproxy_auth_project_info` | 1 with `project_id` when host.auth.list has one (Antigravity) |

Quota, default interval 5 minutes:

| Name | Type |
|------|------|
| `cliproxy_quota_used_ratio` | 0–1 |
| `cliproxy_quota_remaining_ratio` | 0–1 |
| `cliproxy_quota_reset_timestamp_seconds` | unix |
| `cliproxy_quota_has_window` | 1 if this cred has a window, 0 for pay-as-you-go with nothing to plot |
| `cliproxy_quota_supported` | 1 if we know how to fetch this provider |
| `cliproxy_quota_last_success_timestamp_seconds` | unix |
| `cliproxy_quota_poll_interval_seconds` | gauge, default 300 |
| `cliproxy_quota_fetch_errors_total` | counter (`reason`) |
| `cliproxy_quota_reset_credits` | gauge, Codex banked reset credits (`available_count`). Absent when the provider has no such product. |
| `cliproxy_quota_reset_credit_expires_timestamp_seconds` | unix of the soonest *available* credit expiry |

Window ids:

- Codex: `five_hour`, `seven_day`. Reset credits from `/wham/usage` `rate_limit_reset_credits` plus `GET /wham/rate-limit-reset-credits`.
- Claude: `five_hour`, `seven_day` when a Claude cred exists
- Kimi: `five_hour` / `weekly` when a Kimi cred exists
- Antigravity: `gemini_weekly`, `claude_gpt_weekly` (per-model rows from `fetchAvailableModels` are folded into those two)
- Gemini CLI: per-model buckets from `retrieveUserQuota` when a gemini-cli cred exists
- xAI: `weekly`. `grok_build` only if the billing JSON actually contains it. xAI has no Codex-style banked reset-credit count in `cli-chat-proxy` billing.

Quota HTTP fetch (used/remaining/reset + supported): Claude, Codex, Antigravity, Gemini CLI, Kimi, xAI. Other CPA providers (Qwen, iFlow, Vertex, API keys, openai-compatibility) still emit **usage** series (`cliproxy_requests_total` / tokens / duration / failures) as soon as `usage.handle` fires; they stay `cliproxy_quota_supported=0` until we have a stable quota URL.

`usage.handle` is not limited to the accounts currently logged in on one homelab. A Claude-only install gets `provider="claude"` on the same metric names.

Labels used: `provider`, `model`, `auth_index`, `window`, `type`, `status`, `email`, `account_type`, `project_id`. `email` comes from `host.auth.list` (or `unknown` if CPA omitted it). Tokens, cookies, file paths, and raw API keys are dropped.

Per-account fetch failures increment the error counter and leave the last good gauges. They do not take down CPA.

## Grafana

Dashboard JSON: [`grafana/cliproxy.json`](grafana/cliproxy.json). UID `cliproxy-quota`. Import it and pick a Prometheus datasource (variable defaults to uid `mimir`).

Provider and Email variables default to All (`allValue=.*`). Multi-select queries use `${var:regex}`.

```bash
python3 grafana/build.py
```

See [`grafana/README.md`](grafana/README.md).

## Install

Third-party Plugin Store source (the official registry stays):

```
https://raw.githubusercontent.com/giovannirco/cpa-prometheus-plugin/main/registry.json
```

```yaml
plugins:
  enabled: true
  dir: /CLIProxyAPI/plugins
  store-sources:
    - "https://raw.githubusercontent.com/giovannirco/cpa-prometheus-plugin/main/registry.json"
  configs:
    cpa-prometheus:
      enabled: true
      priority: 50
      quota-refresh-interval: 5m
      public-metrics: false
      # scrape-token: "<dedicated token>"  # if set, Bearer / X-Scrape-Token required
```

If `config.yaml` is a read-only Secret, `enabled: true` has to be in that file already. Store Install cannot persist config onto a read-only mount.

Release zips are `cpa-prometheus_<version>_<goos>_<goarch>.zip` with the library at the zip root (`cpa-prometheus.so` / `.dylib`) plus a combined `checksums.txt`. CPA installs linux/amd64 as `plugins/linux/amd64/cpa-prometheus-v<version>.so`. Restart if you get a loaded-plugin lock. Current GHA matrix: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. No Windows zip.

Store Install uses the GitHub API and 403s when unauthenticated rate limits kick in. Download the zip, check the sha256, and copy the library into the plugins directory yourself.

Build `c-shared` on GitHub Actions (`release.yml`). Qemu linux/amd64 on a Mac has segfaulted mid-compile; don't ship those leftovers.

## Config

| Key | Default |
|-----|---------|
| `quota-refresh-interval` | `5m` |
| `request-timeout` | `20s` |
| `include-disabled` | `false` |
| `public-metrics` | `false` (resource `/metrics` is 401 unless true or a scrape-token is presented) |
| `scrape-token` | empty |

## Build

```bash
go test ./...
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o dist/cpa-prometheus.so ./cmd/plugin
make VERSION=0.1.8 package   # zip + checksums; needs the .so from `make build`
```

## License

MIT
