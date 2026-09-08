package plugin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

func TestUsageHandleIncrementsShippedCollector(t *testing.T) {
	rt := NewRuntime(nil)
	_ = rt.Handle("plugin.register", nil)
	usage := []byte(`{"Provider":"xai","Model":"grok-4.6","APIKey":"sk-secret-should-never-label","Latency":1250000000,"Failed":false,"Detail":{"InputTokens":10,"OutputTokens":20,"TotalTokens":30}}`)
	raw := rt.Handle("usage.handle", usage)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("env=%s", raw)
	}
	text, err := rt.Collector().Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_requests_total") || !strings.Contains(text, `model="grok-4.6"`) {
		t.Fatalf("collector text missing usage series:\n%s", text)
	}
	if strings.Contains(text, "sk-secret") {
		t.Fatalf("api key leaked:\n%s", text)
	}
}

func TestUsageHandleWritesLastRequestTimestamp(t *testing.T) {
	rt := NewRuntime(nil)
	_ = rt.Handle("plugin.register", nil)
	usage := []byte(`{"Provider":"xai","Model":"grok-4.6","AuthIndex":"a1","RequestedAt":"2023-11-14T22:13:20Z","Latency":1000000,"Failed":false,"Detail":{"InputTokens":1,"TotalTokens":1}}`)
	raw := rt.Handle("usage.handle", usage)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("env=%s", raw)
	}
	text, err := rt.Collector().Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_last_request_timestamp_seconds") {
		t.Fatalf("last_request missing:\n%s", text)
	}
	if !strings.Contains(text, "1.7e+09") && !strings.Contains(text, "1700000000") {
		t.Fatalf("RequestedAt unix missing:\n%s", text)
	}
}

func TestResourceMetricsPathIsNotServed(t *testing.T) {
	rt := NewRuntime(nil)
	_ = rt.Handle("plugin.register", nil)
	for _, path := range []string{
		"/v0/resource/plugins/cpa-prometheus/metrics",
		"/v0/resource/plugins/cpa-prometheus/metrics/",
		"/v0/RESOURCE/plugins/cpa-prometheus/metrics",
	} {
		raw := rt.Handle("management.handle", []byte(`{"Method":"GET","Path":"`+path+`"}`))
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
			t.Fatalf("%s", raw)
		}
		var resp managementResponse
		if err := json.Unmarshal(env.Result, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 404 {
			t.Fatalf("%s status %d, want 404", path, resp.StatusCode)
		}
		if strings.Contains(string(resp.Body), "cliproxy_") {
			t.Fatalf("%s leaked metrics: %s", path, resp.Body)
		}
	}
}

func TestManagementHandleRejectsEmptyPath(t *testing.T) {
	rt := NewRuntime(nil)
	_ = rt.Handle("plugin.register", nil)
	raw := rt.Handle("management.handle", []byte(`{"Method":"GET"}`))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("%s", raw)
	}
	var resp managementResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("empty path status %d, want 404 (metrics need an explicit route)", resp.StatusCode)
	}
	if strings.Contains(string(resp.Body), "cliproxy_") {
		t.Fatalf("empty path leaked metrics: %s", resp.Body)
	}
}

func TestRegisterDoesNotAdvertiseRequestTimeout(t *testing.T) {
	rt := NewRuntime(nil)
	raw := rt.Handle("plugin.register", nil)
	// request-timeout cannot be honoured: host.http.do takes no timeout, so
	// the field must not be offered as if it were a working control.
	if strings.Contains(string(raw), "request-timeout") {
		t.Fatalf("register still advertises request-timeout: %s", raw)
	}
}

func TestManagementRegisterAdvertisesNoResources(t *testing.T) {
	rt := NewRuntime(nil)
	raw := rt.Handle("management.register", nil)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("%s", raw)
	}
	var reg struct {
		Routes    []map[string]string `json:"routes"`
		Resources []map[string]string `json:"resources"`
	}
	if err := json.Unmarshal(env.Result, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.Resources) != 0 {
		t.Fatalf("plugin must register no resource routes, got %v", reg.Resources)
	}
	if len(reg.Routes) != 1 || reg.Routes[0]["Path"] != metricsManagePath {
		t.Fatalf("routes = %v, want only %s", reg.Routes, metricsManagePath)
	}
}

func TestRegisterExposesNoScrapeAuthConfigFields(t *testing.T) {
	rt := NewRuntime(nil)
	raw := rt.Handle("plugin.register", nil)
	if strings.Contains(string(raw), "public-metrics") || strings.Contains(string(raw), "scrape-token") {
		t.Fatalf("register metadata still advertises resource scrape auth: %s", raw)
	}
}

func TestManagementMetricsServedOnManagementRoute(t *testing.T) {
	rt := NewRuntime(nil)
	_ = rt.Handle("plugin.register", nil)
	raw := rt.Handle("management.handle", []byte(`{"Method":"GET","Path":"/v0/management/plugins/cpa-prometheus/metrics"}`))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("%s", raw)
	}
	var resp managementResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("management metrics status %d, want 200 (host already authenticated)", resp.StatusCode)
	}
}

func TestManagementHandleServesRealMetricsHandler(t *testing.T) {
	rt := NewRuntime(nil)
	_ = rt.Handle("plugin.register", nil)
	_ = rt.Handle("usage.handle", []byte(`{"Provider":"claude","Model":"claude-sonnet-4","Latency":800000000,"Failed":true,"Failure":{"StatusCode":429},"Detail":{"InputTokens":1,"TotalTokens":1}}`))
	rt.Collector().ApplyQuota([]quota.Account{{
		Provider: "claude", AuthIndex: "a1", Supported: true,
		Windows:   []quota.Window{{ID: "five_hour", UsedRatio: 0.2, RemainingRatio: 0.8, ResetUnix: 1700000000}},
		FetchedAt: time.Unix(1700000000, 0).UTC(),
	}})
	rt.Collector().ApplyCredentials([]quota.Credential{{Provider: "claude", Status: "active"}})
	rt.Collector().SetPollInterval(5 * time.Minute)
	raw := rt.Handle("management.handle", []byte(`{"Method":"GET","Path":"/v0/management/plugins/cpa-prometheus/metrics"}`))
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("env=%s", raw)
	}
	var resp managementResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d body %s", resp.StatusCode, resp.Body)
	}
	ct := strings.Join(resp.Headers["Content-Type"], " ")
	if ct == "" {
		ct = strings.Join(resp.Headers["content-type"], " ")
	}
	if !strings.Contains(strings.ToLower(ct), "text/plain") {
		t.Fatalf("Content-Type=%q headers=%v", ct, resp.Headers)
	}
	body := string(resp.Body)
	for _, name := range []string{
		"cliproxy_requests_total",
		"cliproxy_failures_total",
		"cliproxy_tokens_total",
		"cliproxy_request_duration_seconds",
		"cliproxy_quota_poll_interval_seconds",
		"cliproxy_info",
		"cliproxy_credentials",
		"cliproxy_quota_used_ratio",
	} {
		if !strings.Contains(body, name) {
			t.Fatalf("metrics body missing %s:\n%s", name, body)
		}
	}
}

func TestRegisterDefaultIntervalFiveMinutes(t *testing.T) {
	rt := NewRuntime(nil)
	raw := rt.Handle("plugin.register", nil)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
		t.Fatalf("%s", raw)
	}
	text, err := rt.Collector().Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_quota_poll_interval_seconds") {
		t.Fatalf("%s", text)
	}
	if !strings.Contains(text, "300") {
		t.Fatalf("expected 300s default interval:\n%s", text)
	}
}
