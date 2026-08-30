# CPA Prometheus

Native [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) plugin that exposes Prometheus metrics for:

- Management Center dashboard counts (credentials, models seen, plugin info)
- `usage.handle` request / token / latency / failure series
- Provider quota windows on a **5 minute** poll (Claude, Codex, Antigravity, Kimi, xAI/Grok)

It is an in-process C ABI `.so`. It does **not** add host `GET /metrics` (that stays 404).

## Scrape

```
GET /v0/resource/plugins/cpa-prometheus/metrics
```

Optional `scrape-token` config requires `Authorization: Bearer <token>` or `X-Scrape-Token`.

## Install (Plugin Store)

Add this registry as a third-party source (`plugins.store-sources`). The official store is always kept.

```
https://raw.githubusercontent.com/giovannirco/cpa-prometheus-plugin/main/registry.json
```

Then install `cpa-prometheus` from that source. GitOps must already set `plugins.configs.cpa-prometheus.enabled: true` if `config.yaml` is a read-only Secret.

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

Drop the linux/amd64 library at zip-root `cpa-prometheus.so` (store writes `plugins/linux/amd64/cpa-prometheus-v<version>.so`). Restart CPA after install if it reports a loaded-plugin lock.

## Labels

Series use `provider`, `model`, `auth_index`, `window`, `type`, `status`. Never tokens, emails, cookies, or raw API keys.

## Build

```bash
go test ./...
# linux/amd64 plugin (needs CGO + a C compiler)
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o dist/cpa-prometheus.so ./cmd/plugin
```

Compatible with CLIProxyAPI v7.2.x plugin-capable builds (not `_no-plugin`).
