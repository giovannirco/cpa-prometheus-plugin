# Changelog

## 0.2.1 — 2026-09-08

- Ship `windows/amd64`. Releases now carry five zips (linux amd64/arm64, darwin amd64/arm64, windows amd64); the Windows zip holds `cpa-prometheus.dll` at its root. Cross-built with mingw-w64 on the Linux runner and, like every target, unverified on the host OS beyond the zip-layout check.
- CI cross-builds every Linux-buildable target on each PR and asserts the packaged zip layout, so a broken cgo target surfaces before a tag instead of during a release.
- `ValidatePlatformZip` / `LibraryExt` replace the linux-only zip check; tests cover all three extensions and reject a zip validated against the wrong GOOS. `ValidateLinuxZip` stays as a thin wrapper.

No behavior change to the plugin itself: metrics, routes, labels, and config keys are exactly as in 0.2.0.

## 0.2.0 — 2026-09-08

- **Breaking:** the resource metrics endpoint is removed. `GET /v0/resource/plugins/cpa-prometheus/metrics` is no longer registered or served, and `management.register` advertises no `resources` entry. CPA resource routes are not management-authenticated and store policy limits them to static deployed assets ([store PR #111 review](https://github.com/router-for-me/CLIProxyAPI-Plugins-Store/pull/111), repo issue #1).
- **Breaking:** the `public-metrics` and `scrape-token` config keys are removed, along with the plugin-side scrape auth they guarded. Leftover keys in `config.yaml` are ignored rather than rejected.
- Metrics are served exclusively from `GET /v0/management/plugins/cpa-prometheus/metrics`, which CPA protects with the management key. Scrape with `bearer_token` / `bearerTokenSecret` holding that key.
- No metric names, labels, or values changed; the Grafana dashboard needs only a scrape-path update.

## 0.1.8 — 2026-08-30

- Resource `GET /v0/resource/plugins/cpa-prometheus/metrics` is **closed by default** (401). Matches CPA store guidance: resource routes are not management-authenticated.
- Plugin config tab: `public-metrics` (boolean, default false) to opt into unauthenticated LAN scrape.
- `scrape-token` still works: when set, resource scrape requires `Authorization: Bearer` or `X-Scrape-Token`.
- Management `GET /v0/management/plugins/cpa-prometheus/metrics` stays open at the plugin layer (CPA already requires the management key).
- Multi-platform release zips: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64.
- Plugin store logo (`logo.png`).

## 0.1.7 — 2026-08-30

- `cliproxy_quota_reset_credits` and `cliproxy_quota_reset_credit_expires_timestamp_seconds` for Codex banked reset credits.
- Gemini CLI quota fetch via `retrieveUserQuota`.
- Grafana dashboard: reset-credits stat, timeseries, and credentials-table columns.

## 0.1.6 — 2026-08-30

- Security pass: CGO bounds, token-shaped labels dropped, quota HTTPS allowlist, YAML size/anchor cap.
- CI on Go 1.26.7 + govulncheck.

## 0.1.5 — 2026-08-30

- `email` / `account_type` labels, last-request timestamp, `updated_at`, Antigravity `project_id`.
