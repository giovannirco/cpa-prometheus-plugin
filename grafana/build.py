#!/usr/bin/env python3
"""Generate grafana/cliproxy.json (public SoT) and optional GitOps ConfigMap."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

UID = "cliproxy-quota"
TITLE = "CLIProxyAPI / CPA Prometheus"
PLUGIN = 'plugin_id="cpa-prometheus"'
SEL = f'{PLUGIN},provider=~"${{provider:regex}}",email=~"${{email:regex}}"'
SEL_P = f'{PLUGIN},provider=~"${{provider:regex}}"'
DS = {"type": "prometheus", "uid": "${datasource}"}
HIDE = {
    "Time": True,
    "__name__": True,
    "plugin_id": True,
    "job": True,
    "instance": True,
    "namespace": True,
    "pod": True,
    "container": True,
    "cluster": True,
    "k8s_cluster_name": True,
}
# Quota poll is 5m and Alloy scrapes ~1m; connect steps across a missed scrape.
QUOTA_SPAN_MS = 660000


def tgt(expr: str, ref: str = "A", legend: str = "", instant: bool = False, fmt: str | None = None) -> dict:
    out: dict = {
        "datasource": DS,
        "expr": expr,
        "refId": ref,
        "legendFormat": legend,
    }
    if instant:
        out["instant"] = True
        out["format"] = fmt or "table"
    elif fmt:
        out["format"] = fmt
    return out


def grid(h: int, w: int, x: int, y: int) -> dict:
    return {"h": h, "w": w, "x": x, "y": y}


def thresh(*steps: tuple[str, float | None]) -> dict:
    return {
        "mode": "absolute",
        "steps": [{"color": c, "value": v} for c, v in steps],
    }


def row(pid: int, title: str, y: int) -> dict:
    return {
        "id": pid,
        "type": "row",
        "title": title,
        "collapsed": False,
        "gridPos": grid(1, 24, 0, y),
        "panels": [],
    }


def stat(
    pid: int,
    title: str,
    expr: str,
    x: int,
    y: int,
    *,
    w: int = 4,
    h: int = 4,
    desc: str = "",
    unit: str = "short",
    decimals: int | None = 0,
    steps: tuple[tuple[str, float | None], ...] = (("blue", None),),
    mappings: list | None = None,
    legend: str = "",
) -> dict:
    defaults: dict = {
        "color": {"mode": "thresholds"},
        "thresholds": thresh(*steps),
        "unit": unit,
        "mappings": mappings or [],
    }
    if decimals is not None:
        defaults["decimals"] = decimals
    return {
        "id": pid,
        "type": "stat",
        "title": title,
        "description": desc,
        "gridPos": grid(h, w, x, y),
        "datasource": DS,
        "targets": [tgt(expr, legend=legend or title)],
        "fieldConfig": {"defaults": defaults, "overrides": []},
        "options": {
            "colorMode": "background",
            "graphMode": "area",
            "justifyMode": "auto",
            "orientation": "auto",
            "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
            "textMode": "auto",
        },
    }


def ts_custom(*, span: int | bool = False, step: bool = False, stack: str = "none", fill: int = 12) -> dict:
    return {
        "axisCenteredZero": False,
        "axisColorMode": "text",
        "axisLabel": "",
        "axisPlacement": "auto",
        "barAlignment": 0,
        "drawStyle": "line",
        "fillOpacity": fill,
        "gradientMode": "none",
        "hideFrom": {"legend": False, "tooltip": False, "viz": False},
        "lineInterpolation": "stepAfter" if step else "smooth",
        "lineWidth": 2,
        "pointSize": 5,
        "scaleDistribution": {"type": "linear"},
        "showPoints": "never",
        "spanNulls": span,
        "stacking": {"group": "A", "mode": stack},
        "thresholdsStyle": {"mode": "off"},
    }


def timeseries(
    pid: int,
    title: str,
    targets: list[dict],
    x: int,
    y: int,
    *,
    w: int = 12,
    h: int = 8,
    desc: str = "",
    unit: str = "short",
    min_v: float | None = None,
    max_v: float | None = None,
    span: int | bool = False,
    step: bool = False,
    stack: str = "none",
    steps: tuple[tuple[str, float | None], ...] = (("green", None),),
    calcs: list[str] | None = None,
) -> dict:
    defaults: dict = {
        "color": {"mode": "palette-classic"},
        "custom": ts_custom(span=span, step=step, stack=stack),
        "unit": unit,
        "thresholds": thresh(*steps),
        "mappings": [],
    }
    if min_v is not None:
        defaults["min"] = min_v
    if max_v is not None:
        defaults["max"] = max_v
    return {
        "id": pid,
        "type": "timeseries",
        "title": title,
        "description": desc,
        "gridPos": grid(h, w, x, y),
        "datasource": DS,
        "targets": targets,
        "fieldConfig": {"defaults": defaults, "overrides": []},
        "options": {
            "legend": {
                "calcs": calcs or ["lastNotNull", "max"],
                "displayMode": "table",
                "placement": "bottom",
                "showLegend": True,
            },
            "tooltip": {"mode": "multi", "sort": "desc"},
        },
    }


def gauge(pid: int, title: str, expr: str, legend: str, x: int, y: int, *, w: int = 8, h: int = 8, desc: str = "") -> dict:
    return {
        "id": pid,
        "type": "gauge",
        "title": title,
        "description": desc,
        "gridPos": grid(h, w, x, y),
        "datasource": DS,
        "targets": [tgt(expr, legend=legend)],
        "fieldConfig": {
            "defaults": {
                "color": {"mode": "thresholds"},
                "thresholds": thresh(("green", None), ("yellow", 0.7), ("orange", 0.85), ("red", 0.95)),
                "unit": "percentunit",
                "min": 0,
                "max": 1,
                "decimals": 2,
                "mappings": [],
            },
            "overrides": [],
        },
        "options": {
            "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
            "showThresholdLabels": False,
            "showThresholdMarkers": True,
            "orientation": "auto",
        },
    }


def table(
    pid: int,
    title: str,
    targets: list[dict],
    overrides: list[dict],
    x: int,
    y: int,
    *,
    w: int = 24,
    h: int = 8,
    desc: str = "",
    sort: str | None = None,
) -> dict:
    opts: dict = {
        "showHeader": True,
        "cellHeight": "sm",
        "footer": {"show": False, "reducer": ["sum"], "countRows": False, "fields": ""},
    }
    if sort:
        opts["sortBy"] = [{"displayName": sort, "desc": True}]
    return {
        "id": pid,
        "type": "table",
        "title": title,
        "description": desc,
        "gridPos": grid(h, w, x, y),
        "datasource": DS,
        "targets": targets,
        "fieldConfig": {
            "defaults": {
                "color": {"mode": "thresholds"},
                "thresholds": thresh(("green", None)),
                "mappings": [],
                "custom": {"align": "auto", "cellOptions": {"type": "auto"}, "inspect": False, "filterable": True},
            },
            "overrides": overrides,
        },
        "options": opts,
        "transformations": [
            {"id": "merge", "options": {}},
            {"id": "organize", "options": {"excludeByName": HIDE, "indexByName": {}, "renameByName": {}}},
        ],
    }


def heatmap(pid: int, title: str, expr: str, x: int, y: int, *, w: int = 12, h: int = 8, desc: str = "") -> dict:
    return {
        "id": pid,
        "type": "heatmap",
        "title": title,
        "description": desc,
        "gridPos": grid(h, w, x, y),
        "datasource": DS,
        "targets": [tgt(expr, legend="{{le}}", fmt="heatmap")],
        "fieldConfig": {
            "defaults": {"custom": {"scaleDistribution": {"type": "log", "log": 2}}},
            "overrides": [],
        },
        "options": {
            "calculate": False,
            "cellGap": 1,
            "color": {
                "exponent": 0.5,
                "fill": "dark-orange",
                "mode": "scheme",
                "reverse": False,
                "scale": "exponential",
                "scheme": "Oranges",
                "steps": 64,
            },
            "exemplars": {"color": "rgba(255,0,255,0.7)"},
            "filterValues": {"le": 1e-09},
            "tooltip": {"mode": "single", "yHistogram": True},
            "yAxis": {"axisLabel": "Duration", "axisPlacement": "left", "reverse": False, "unit": "s"},
            "legend": {"show": True},
        },
    }


def val_override(name: str, display: str, unit: str | None = None, decimals: int | None = None, color_bg: bool = False, steps=None) -> dict:
    props = [{"id": "displayName", "value": display}]
    if unit:
        props.append({"id": "unit", "value": unit})
    if decimals is not None:
        props.append({"id": "decimals", "value": decimals})
    if color_bg:
        props.append({"id": "custom.cellOptions", "value": {"type": "color-background", "mode": "basic"}})
    if steps:
        props.append({"id": "thresholds", "value": thresh(*steps)})
    return {"matcher": {"id": "byName", "options": name}, "properties": props}


def dashboard() -> dict:
    y0 = 1
    y1 = 6
    quota_y = 11
    windows_y = 12
    used_y = 20
    qtable_y = 28
    qerr_y = 36
    acct_y = 45
    cred_y = 46
    host_y = 54
    flag_y = 62
    usage_y = 71
    req_y = 72
    lat_y = 80
    models_y = 88

    panels: list[dict] = [
        row(1, "Overview", 0),
        stat(
            2,
            "Plugin up",
            f"max(cliproxy_up{{{PLUGIN}}})",
            0,
            y0,
            desc="1 if cpa-prometheus is loaded.",
            steps=(("red", None), ("green", 1)),
            mappings=[
                {
                    "type": "value",
                    "options": {
                        "0": {"text": "down", "color": "red"},
                        "1": {"text": "up", "color": "green"},
                    },
                }
            ],
        ),
        stat(
            3,
            "Plugin version",
            f"max by (plugin_version) (cliproxy_info{{{PLUGIN}}})",
            4,
            y0,
            desc="cliproxy_info plugin_version.",
            decimals=None,
            legend="{{plugin_version}}",
        ),
        stat(
            4,
            "Credentials",
            f"sum(cliproxy_credentials{{{SEL}}})",
            8,
            y0,
            desc="Accounts from host.auth.list matching the filters.",
        ),
        stat(
            5,
            "Requests in range",
            f'sum(increase(cliproxy_requests_total{{{SEL}}}[$__range])) or vector(0)',
            12,
            y0,
            desc="Completed usage.handle records in the dashboard range.",
        ),
        stat(
            6,
            "Tokens in range",
            f'sum(increase(cliproxy_tokens_total{{{SEL},type="total"}}[$__range])) or vector(0)',
            16,
            y0,
            desc="type=total only so input/output/reasoning are not double-counted.",
        ),
        stat(
            7,
            "Failures in range",
            f"sum(increase(cliproxy_failures_total{{{SEL}}}[$__range])) or vector(0)",
            20,
            y0,
            desc="Failed usage.handle records. Stays 0 until a failure is observed.",
            steps=(("green", None), ("yellow", 1), ("red", 5)),
        ),
        stat(
            8,
            "Last request age",
            f"min(time() - cliproxy_last_request_timestamp_seconds{{{SEL}}})",
            0,
            y1,
            w=4,
            desc="Youngest usage.handle timestamp in the filter. Empty until the first request after a restart.",
            unit="dtdurations",
            decimals=None,
            steps=(("green", None), ("yellow", 3600), ("orange", 21600), ("red", 86400)),
        ),
        stat(
            9,
            "Models seen",
            f"sum(cliproxy_models_seen{{{SEL_P}}})",
            4,
            y1,
            desc="Distinct models observed via usage.handle.",
        ),
        stat(
            10,
            "Disabled",
            f"sum(cliproxy_auth_disabled{{{SEL}}})",
            8,
            y1,
            desc="host.auth.list disabled=1.",
            steps=(("green", None), ("red", 1)),
        ),
        stat(
            11,
            "Unavailable",
            f"sum(cliproxy_auth_unavailable{{{SEL}}})",
            12,
            y1,
            desc="host.auth.list unavailable=1.",
            steps=(("green", None), ("orange", 1)),
        ),
        stat(
            12,
            "PAYG / no window",
            f"count(cliproxy_quota_has_window{{{SEL}}} == 0) or vector(0)",
            16,
            y1,
            desc="Credentials with cliproxy_quota_has_window=0 (pay-as-you-go or empty payload).",
        ),
        stat(
            13,
            "Poll interval",
            f"max(cliproxy_quota_poll_interval_seconds{{{PLUGIN}}})",
            20,
            y1,
            desc="Configured quota refresh interval.",
            unit="s",
            decimals=None,
        ),
        row(20, "Quota", quota_y),
        gauge(
            21,
            "Quota used",
            f"cliproxy_quota_used_ratio{{{SEL}}}",
            "{{provider}} {{email}} {{window}}",
            0,
            windows_y,
            desc="0-1 per account window. Red at 95% used. PAYG accounts with no window do not appear.",
        ),
        timeseries(
            22,
            "Quota remaining",
            [tgt(f"cliproxy_quota_remaining_ratio{{{SEL}}}", legend="{{provider}} {{email}} {{window}}")],
            8,
            windows_y,
            w=16,
            desc="1 is unused. Snapshots every quota poll (~5m).",
            unit="percentunit",
            min_v=0,
            max_v=1,
            span=QUOTA_SPAN_MS,
            step=True,
        ),
        timeseries(
            23,
            "Quota used over time",
            [tgt(f"cliproxy_quota_used_ratio{{{SEL}}}", legend="{{provider}} {{email}} {{window}}")],
            0,
            used_y,
            desc="Same windows as the gauge, as a history.",
            unit="percentunit",
            min_v=0,
            max_v=1,
            span=QUOTA_SPAN_MS,
            step=True,
            steps=(("green", None), ("yellow", 0.7), ("red", 0.95)),
        ),
        timeseries(
            24,
            "Time until window reset",
            [tgt(f"cliproxy_quota_reset_timestamp_seconds{{{SEL}}} - time()", legend="{{provider}} {{email}} {{window}}")],
            12,
            used_y,
            desc="Seconds until the provider window resets. Negative means the stamp is in the past.",
            unit="dtdurations",
            span=QUOTA_SPAN_MS,
            step=True,
        ),
        table(
            25,
            "Quota windows",
            [
                tgt(f"cliproxy_quota_used_ratio{{{SEL}}}", "A", instant=True),
                tgt(f"cliproxy_quota_remaining_ratio{{{SEL}}}", "B", instant=True),
                tgt(f"cliproxy_quota_reset_timestamp_seconds{{{SEL}}} - time()", "C", instant=True),
            ],
            [
                val_override(
                    "Value #A",
                    "Used",
                    unit="percentunit",
                    decimals=2,
                    color_bg=True,
                    steps=(("green", None), ("yellow", 0.7), ("orange", 0.85), ("red", 0.95)),
                ),
                val_override("Value #B", "Remaining", unit="percentunit", decimals=2),
                val_override("Value #C", "Until reset", unit="dtdurations"),
            ],
            0,
            qtable_y,
            h=8,
            desc="Per window (provider, email, auth_index, window). has_window lives on the credentials table.",
            sort="Used",
        ),
        timeseries(
            26,
            "Quota fetch errors",
            [
                tgt(
                    f"sum by (provider, reason) (increase(cliproxy_quota_fetch_errors_total{{{SEL_P}}}[$__rate_interval]))",
                    legend="{{provider}} {{reason}}",
                )
            ],
            0,
            qerr_y,
            w=16,
            desc="Isolated per provider. Empty while fetches succeed. Does not take CPA down.",
        ),
        timeseries(
            27,
            "Seconds since last successful fetch",
            [
                tgt(
                    f"time() - cliproxy_quota_last_success_timestamp_seconds{{{SEL}}}",
                    legend="{{provider}} {{email}}",
                )
            ],
            16,
            qerr_y,
            w=8,
            desc="Should stay near the poll interval. Climbing means fetches are failing.",
            unit="dtdurations",
            span=QUOTA_SPAN_MS,
            step=True,
            steps=(("green", None), ("yellow", 600), ("red", 1800)),
        ),
        row(30, "Accounts", acct_y),
        table(
            31,
            "Credentials",
            [
                tgt(f"cliproxy_credentials{{{SEL}}}", "A", instant=True),
                tgt(f"cliproxy_quota_has_window{{{SEL}}}", "B", instant=True),
                tgt(f"cliproxy_quota_supported{{{SEL}}}", "C", instant=True),
                tgt(f"cliproxy_auth_disabled{{{SEL}}}", "D", instant=True),
                tgt(f"cliproxy_auth_unavailable{{{SEL}}}", "E", instant=True),
                tgt(f"cliproxy_auth_runtime_only{{{SEL}}}", "F", instant=True),
                tgt(f"cliproxy_auth_success{{{SEL}}}", "G", instant=True),
                tgt(f"cliproxy_auth_failed{{{SEL}}}", "H", instant=True),
                tgt(f"time() - cliproxy_auth_updated_timestamp_seconds{{{SEL}}}", "I", instant=True),
                tgt(f"time() - cliproxy_auth_last_refresh_timestamp_seconds{{{SEL}}}", "J", instant=True),
                tgt(f"cliproxy_auth_project_info{{{SEL}}}", "K", instant=True),
            ],
            [
                val_override("Value #A", "Count"),
                val_override("Value #B", "Has window"),
                val_override("Value #C", "Quota supported"),
                val_override(
                    "Value #D",
                    "Disabled",
                    color_bg=True,
                    steps=(("green", None), ("red", 1)),
                ),
                val_override(
                    "Value #E",
                    "Unavailable",
                    color_bg=True,
                    steps=(("green", None), ("orange", 1)),
                ),
                val_override("Value #F", "Runtime only"),
                val_override("Value #G", "Host success"),
                val_override("Value #H", "Host failed"),
                val_override("Value #I", "Updated age", unit="dtdurations"),
                val_override("Value #J", "Refresh age", unit="dtdurations"),
                val_override("Value #K", "Has project"),
            ],
            0,
            cred_y,
            h=8,
            desc="Per account from host.auth.list. success/failed are host snapshots, not Prom counters. Refresh age is empty when CPA omits last_refresh.",
        ),
        timeseries(
            32,
            "Host success / failed (snapshot)",
            [
                tgt(f"cliproxy_auth_success{{{SEL}}}", "A", legend="ok {{provider}} {{email}}"),
                tgt(f"cliproxy_auth_failed{{{SEL}}}", "B", legend="fail {{provider}} {{email}}"),
            ],
            0,
            host_y,
            desc="CPA host.auth.list counters. These are gauges (latest snapshot), not monotonically increasing Prom counters.",
            span=QUOTA_SPAN_MS,
            step=True,
        ),
        timeseries(
            33,
            "Seconds since last request",
            [
                tgt(
                    f"time() - cliproxy_last_request_timestamp_seconds{{{SEL}}}",
                    legend="{{provider}} {{email}} {{model}}",
                )
            ],
            12,
            host_y,
            desc="Per provider, model, and credential. Empty until usage.handle fires after a restart.",
            unit="dtdurations",
        ),
        timeseries(
            34,
            "Disabled / unavailable / runtime-only",
            [
                tgt(f"cliproxy_auth_disabled{{{SEL}}}", "A", legend="disabled {{provider}} {{email}}"),
                tgt(f"cliproxy_auth_unavailable{{{SEL}}}", "B", legend="unavailable {{provider}} {{email}}"),
                tgt(f"cliproxy_auth_runtime_only{{{SEL}}}", "C", legend="runtime_only {{provider}} {{email}}"),
            ],
            0,
            flag_y,
            desc="0/1 gauges from host.auth.list.",
            min_v=0,
            max_v=1,
            span=QUOTA_SPAN_MS,
            step=True,
        ),
        timeseries(
            35,
            "Cooldown retry (when set)",
            [
                tgt(
                    f"cliproxy_auth_next_retry_timestamp_seconds{{{SEL}}} - time()",
                    legend="{{provider}} {{email}}",
                )
            ],
            12,
            flag_y,
            desc="Only present while the credential is cooling down. Empty is healthy.",
            unit="dtdurations",
            span=QUOTA_SPAN_MS,
            step=True,
        ),
        row(40, "Usage", usage_y),
        timeseries(
            41,
            "Request rate",
            [
                tgt(
                    f"sum by (provider, email, model) (rate(cliproxy_requests_total{{{SEL}}}[$__rate_interval]))",
                    legend="{{provider}} {{email}} {{model}}",
                )
            ],
            0,
            req_y,
            desc="Completed usage.handle records.",
            unit="reqps",
        ),
        timeseries(
            42,
            "Tokens by type",
            [
                tgt(
                    f'sum by (provider, email, type) (rate(cliproxy_tokens_total{{{SEL},type!~"total|cached"}}[$__rate_interval]))',
                    legend="{{provider}} {{email}} {{type}}",
                )
            ],
            12,
            req_y,
            desc="Excludes type=total (duplicate of the parts) and type=cached (duplicate of cache_read).",
            unit="ops",
            stack="normal",
        ),
        timeseries(
            43,
            "Latency p50 / p95",
            [
                tgt(
                    f"histogram_quantile(0.50, sum by (le, provider, model) (rate(cliproxy_request_duration_seconds_bucket{{{SEL}}}[$__rate_interval])))",
                    "A",
                    "p50 {{provider}} {{model}}",
                ),
                tgt(
                    f"histogram_quantile(0.95, sum by (le, provider, model) (rate(cliproxy_request_duration_seconds_bucket{{{SEL}}}[$__rate_interval])))",
                    "B",
                    "p95 {{provider}} {{model}}",
                ),
            ],
            0,
            lat_y,
            desc="From usage.handle duration histogram.",
            unit="s",
        ),
        heatmap(
            44,
            "Latency heatmap",
            f"sum by (le) (increase(cliproxy_request_duration_seconds_bucket{{{SEL}}}[$__rate_interval]))",
            12,
            lat_y,
            desc="Distribution of request duration buckets. Empty until usage.handle records exist.",
        ),
        timeseries(
            45,
            "Failures by code",
            [
                tgt(
                    f"sum by (provider, email, model, code) (rate(cliproxy_failures_total{{{SEL}}}[$__rate_interval]))",
                    legend="{{provider}} {{email}} {{model}} {{code}}",
                )
            ],
            0,
            models_y,
            w=12,
            desc="Empty until a failed usage.handle record is observed. code is the HTTP status when CPA provides one.",
        ),
        table(
            46,
            "Models observed",
            [
                tgt(f"cliproxy_model_seen{{{SEL_P}}}", "A", instant=True),
                tgt(f"cliproxy_models_seen{{{SEL_P}}}", "B", instant=True),
                tgt(f"cliproxy_model_available{{{SEL}}}", "C", instant=True),
            ],
            [
                val_override("Value #A", "Seen"),
                val_override("Value #B", "Seen count"),
                val_override("Value #C", "Available"),
            ],
            12,
            models_y,
            w=12,
            h=8,
            desc="model_seen is from usage.handle. model_available is emitted only when host.auth.get_runtime includes model_states (often absent on CPA v7).",
        ),
    ]

    return {
        "uid": UID,
        "title": TITLE,
        "description": (
            "cpa-prometheus plugin: quota windows, credentials (email), and usage.handle traffic. "
            "Scrape GET /v0/resource/plugins/cpa-prometheus/metrics. Host GET /metrics stays 404. "
            "https://github.com/giovannirco/cpa-prometheus-plugin"
        ),
        "tags": ["cliproxyapi", "cpa-prometheus", "quota", "usage"],
        "style": "dark",
        "timezone": "browser",
        "editable": True,
        "graphTooltip": 1,
        "schemaVersion": 39,
        "version": 3,
        "refresh": "30s",
        "fiscalYearStartMonth": 0,
        "liveNow": False,
        "links": [
            {
                "title": "Plugin repo",
                "url": "https://github.com/giovannirco/cpa-prometheus-plugin",
                "type": "link",
                "icon": "external link",
                "targetBlank": True,
            }
        ],
        "time": {"from": "now-24h", "to": "now"},
        "timepicker": {
            "refresh_intervals": ["10s", "30s", "1m", "5m"],
            "time_options": ["5m", "15m", "1h", "6h", "12h", "24h", "2d", "7d"],
        },
        "annotations": {
            "list": [
                {
                    "builtIn": 1,
                    "datasource": {"type": "grafana", "uid": "-- Grafana --"},
                    "enable": True,
                    "hide": True,
                    "iconColor": "rgba(0, 211, 255, 1)",
                    "name": "Annotations & Alerts",
                    "type": "dashboard",
                }
            ]
        },
        "templating": {
            "list": [
                {
                    "name": "datasource",
                    "type": "datasource",
                    "query": "prometheus",
                    "label": "Datasource",
                    "hide": 0,
                    "refresh": 1,
                    "current": {"text": "Mimir", "value": "mimir", "selected": True},
                    "options": [],
                },
                {
                    "name": "provider",
                    "type": "query",
                    "datasource": DS,
                    "query": f'label_values(cliproxy_credentials{{{PLUGIN}}}, provider)',
                    "label": "Provider",
                    "includeAll": True,
                    "multi": True,
                    "allValue": ".*",
                    "refresh": 2,
                    "sort": 1,
                    "current": {"text": "All", "value": "$__all", "selected": True},
                    "options": [],
                },
                {
                    "name": "email",
                    "type": "query",
                    "datasource": DS,
                    "query": f'label_values(cliproxy_credentials{{{PLUGIN},provider=~"${{provider:regex}}"}}, email)',
                    "label": "Email",
                    "includeAll": True,
                    "multi": True,
                    "allValue": ".*",
                    "refresh": 2,
                    "sort": 1,
                    "current": {"text": "All", "value": "$__all", "selected": True},
                    "options": [],
                },
            ]
        },
        "panels": panels,
    }


def write_json(path: Path) -> dict:
    dash = dashboard()
    path.write_text(json.dumps(dash, indent=2) + "\n", encoding="utf-8")
    return dash


def write_configmap(dash: dict, path: Path) -> None:
    body = json.dumps(dash, indent=2)
    indented = "\n".join(("    " + line) if line else "    " for line in body.splitlines())
    text = (
        "apiVersion: v1\n"
        "kind: ConfigMap\n"
        "metadata:\n"
        "  name: cliproxy-quota-dashboard\n"
        "  namespace: observability\n"
        "  labels:\n"
        '    grafana_dashboard: "1"\n'
        "  annotations:\n"
        '    grafana_folder: "Homelab"\n'
        "    k8s-sidecar-target-directory: /tmp/dashboards/Homelab\n"
        "data:\n"
        "  cliproxy-quota.json: |-\n"
        f"{indented}\n"
    )
    path.write_text(text, encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--json-out",
        type=Path,
        default=Path(__file__).with_name("cliproxy.json"),
    )
    parser.add_argument("--configmap", type=Path, default=None)
    args = parser.parse_args()
    dash = write_json(args.json_out)
    n_panels = sum(1 for p in dash["panels"] if p["type"] != "row")
    n_rows = sum(1 for p in dash["panels"] if p["type"] == "row")
    print(f"wrote {args.json_out} panels={n_panels} rows={n_rows} bytes={args.json_out.stat().st_size}")
    if args.configmap:
        write_configmap(dash, args.configmap)
        print(f"wrote {args.configmap} bytes={args.configmap.stat().st_size}")


if __name__ == "__main__":
    main()
