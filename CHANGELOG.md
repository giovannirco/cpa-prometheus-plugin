# Changelog

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
