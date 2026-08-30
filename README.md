# CPA Prometheus

CLIProxyAPI plugin that exports Prometheus metrics for proxy traffic and provider quota.

CPA has no `/metrics` of its own. This plugin is an in-process `.so`. It hooks `usage.handle` for request stats, polls quota over `host.auth` + `host.http.do`, and serves text on a resource route. Host `GET /metrics` stays 404.

Needs a plugin-capable CPA v7.2.x image. The `_no-plugin` builds cannot load it.

## Scrape

```
GET /v0/resource/plugins/cpa-prometheus/metrics
```

Resource routes are not management-authenticated, so a scraper can hit this without the management key. Set `scrape-token` if you want a bearer / `X-Scrape-Token` anyway.

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

CPA does not give plugins a model catalog. You only see models that actually went through the proxy.

From `host.auth.list`, refreshed on the quota tick:

| Name | Type |
|------|------|
| `cliproxy_credentials` | gauge by `provider`, `status` |
| `cliproxy_auth_success` | gauge, host snapshot (not a Prom counter) |
| `cliproxy_auth_failed` | gauge, same |
| `cliproxy_auth_disabled` | 0/1 |
| `cliproxy_auth_unavailable` | 0/1 |
| `cliproxy_auth_next_retry_timestamp_seconds` | unix, only while the cred is cooling down |

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

Window ids:

- Codex: `five_hour`, `seven_day`
- Claude: `five_hour`, `seven_day` when a Claude cred exists
- Kimi: `five_hour` / `weekly` when a Kimi cred exists
- Antigravity: `gemini_weekly`, `claude_gpt_weekly` (per-model rows from `fetchAvailableModels` are folded into those two)
- xAI: `weekly`. `grok_build` only if the billing JSON actually contains it

Labels used: `provider`, `model`, `auth_index`, `window`, `type`, `status`. Emails, tokens, cookies, file paths, and raw API keys are dropped.

Per-account fetch failures increment the error counter and leave the last good gauges. They do not take down CPA.

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
```

If `config.yaml` is a read-only Secret, `enabled: true` has to be in that file already. Store Install cannot persist config onto a read-only mount.

Releases are linux/amd64 zip files with `cpa-prometheus.so` at the zip root plus a `checksums.txt`. CPA installs that as `plugins/linux/amd64/cpa-prometheus-v<version>.so`. Restart if you get a loaded-plugin lock.

Store Install uses the GitHub API and 403s when unauthenticated rate limits kick in. Download the zip, check the sha256, and copy the `.so` into the plugins directory yourself.

Build linux/amd64 `c-shared` on GitHub Actions (`release.yml`, ubuntu-latest). Qemu on a Mac has segfaulted for me mid-compile; don't ship those leftovers.

## Config

| Key | Default |
|-----|---------|
| `quota-refresh-interval` | `5m` |
| `request-timeout` | `20s` |
| `include-disabled` | `false` |
| `scrape-token` | empty |

## Build

```bash
go test ./...
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o dist/cpa-prometheus.so ./cmd/plugin
make VERSION=0.1.2 package   # zip + checksums; needs the .so from `make build`
```

## License

MIT
