package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/collector"
	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

// cpaHTTPResponse matches pluginapi.HTTPResponse JSON (no tags → PascalCase).
type cpaHTTPResponse struct {
	StatusCode int
	Body       []byte
}

func TestDoHTTPUnmarshalsCPAShapedHostResult(t *testing.T) {
	body := []byte(`{"five_hour":{"utilization":0.2,"resets_at":"2026-08-30T01:00:00Z"}}`)
	hostResult, err := json.Marshal(cpaHTTPResponse{StatusCode: 200, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostResult), `"StatusCode":200`) {
		t.Fatalf("fixture is not CPA-shaped: %s", hostResult)
	}

	host := NewCallbackHost(func(method string, request []byte) ([]byte, error) {
		if method != "host.http.do" {
			t.Fatalf("method = %s", method)
		}
		return okJSON(json.RawMessage(hostResult)), nil
	})

	got, err := host.DoHTTP(quota.HTTPRequest{Method: "GET", URL: "https://api.anthropic.com/api/oauth/usage"})
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != 200 {
		t.Fatalf("StatusCode = %d, want 200 (CPA marshals pluginapi.HTTPResponse as StatusCode, not status_code)", got.StatusCode)
	}
	if string(got.Body) != string(body) {
		t.Fatalf("Body = %q, want quota JSON", got.Body)
	}
}

func TestPollThroughCallbackHostWritesQuotaGauges(t *testing.T) {
	quotaBody := []byte(`{"five_hour":{"utilization":0.25,"resets_at":"2026-08-30T01:00:00Z"}}`)
	httpResult, err := json.Marshal(cpaHTTPResponse{StatusCode: 200, Body: quotaBody})
	if err != nil {
		t.Fatal(err)
	}

	host := NewCallbackHost(func(method string, request []byte) ([]byte, error) {
		switch method {
		case "host.auth.list":
			return okJSON(map[string]any{
				"files": []map[string]any{{
					"auth_index": "a1",
					"provider":   "claude",
					"status":     "active",
				}},
			}), nil
		case "host.auth.get":
			return okJSON(map[string]any{
				"auth_index": "a1",
				"json":       map[string]any{"access_token": "tok-ok"},
			}), nil
		case "host.http.do":
			return okJSON(json.RawMessage(httpResult)), nil
		default:
			t.Fatalf("unexpected host method %s", method)
			return nil, nil
		}
	})

	accounts, creds, err := quota.Poll(host, quota.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Provider != "claude" {
		t.Fatalf("creds = %#v", creds)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts = %#v", accounts)
	}
	if accounts[0].Error != "" {
		t.Fatalf("quota error %q (StatusCode likely not decoded from CPA JSON)", accounts[0].Error)
	}
	if len(accounts[0].Windows) != 1 || accounts[0].Windows[0].UsedRatio != 0.25 {
		t.Fatalf("windows = %#v", accounts[0].Windows)
	}

	col := collector.New("0.1.0")
	col.ApplyCredentials(creds)
	col.ApplyQuota(accounts)
	text, err := col.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"cliproxy_quota_used_ratio",
		"cliproxy_quota_remaining_ratio",
		"cliproxy_quota_reset_timestamp_seconds",
		"cliproxy_quota_last_success_timestamp_seconds",
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("missing %s:\n%s", name, text)
		}
	}
	if !strings.Contains(text, "0.25") || !strings.Contains(text, "0.75") {
		t.Fatalf("used/remaining ratios missing:\n%s", text)
	}
}
