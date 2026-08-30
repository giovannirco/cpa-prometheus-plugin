package plugin

import (
	"encoding/json"
	"fmt"
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

func TestHostAuthListCopiesEmailNotPath(t *testing.T) {
	host := NewCallbackHost(func(method string, request []byte) ([]byte, error) {
		if method != "host.auth.list" {
			t.Fatalf("method = %s", method)
		}
		return okJSON(map[string]any{
			"files": []map[string]any{{
				"auth_index":       "a1",
				"provider":         "xai",
				"status":           "active",
				"disabled":         false,
				"unavailable":      true,
				"success":          12,
				"failed":           3,
				"next_retry_after": "2027-01-15T00:00:00Z",
				"email":            "gio@example.com",
				"name":             "gio@example.com.json",
				"path":             "/root/.cli-proxy-api/gio@example.com.json",
			}},
		}), nil
	})
	files, err := host.ListAuth()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files=%d", len(files))
	}
	f := files[0]
	if f.Success != 12 || f.Failed != 3 || !f.Unavailable || f.Disabled {
		t.Fatalf("numerics not decoded: %#v", f)
	}
	if f.NextRetryUnix == 0 {
		t.Fatalf("next_retry_after not decoded: %#v", f)
	}
	if f.Email != "gio@example.com" {
		t.Fatalf("email not copied: %#v", f)
	}
	dump := fmt.Sprintf("%#v", f)
	if strings.Contains(dump, "/root/.cli-proxy-api") || strings.Contains(f.AuthIndex, "@") {
		t.Fatalf("path leaked onto AuthFile: %#v", f)
	}
}

func TestHostAuthGetRuntimeDecodesModelStates(t *testing.T) {
	host := NewCallbackHost(func(method string, request []byte) ([]byte, error) {
		if method != "host.auth.get_runtime" {
			t.Fatalf("method = %s", method)
		}
		return okJSON(map[string]any{
			"auth": map[string]any{
				"provider":     "xai",
				"email":        "gio@example.com",
				"account_type": "oauth",
				"model_states": map[string]any{
					"grok-4.6": map[string]any{"status": "active", "unavailable": false},
				},
			},
		}), nil
	})
	rt, err := host.GetRuntime("a1")
	if err != nil {
		t.Fatal(err)
	}
	if rt.Email != "gio@example.com" || rt.AccountType != "oauth" {
		t.Fatalf("runtime %#v", rt)
	}
	if len(rt.Models) != 1 || rt.Models[0].Model != "grok-4.6" {
		t.Fatalf("models %#v", rt.Models)
	}
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
		case "host.auth.get_runtime":
			return okJSON(map[string]any{"auth": map[string]any{
				"auth_index":   "a1",
				"provider":     "claude",
				"email":        "gio@example.com",
				"account_type": "oauth",
			}}), nil
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
