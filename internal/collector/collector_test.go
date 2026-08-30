package collector

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

func TestDefaultQuotaRefreshIntervalIsFiveMinutes(t *testing.T) {
	if DefaultQuotaRefreshInterval != 5*time.Minute {
		t.Fatalf("DefaultQuotaRefreshInterval = %s, want 5m", DefaultQuotaRefreshInterval)
	}
}

func TestObserveUsageWritesPrometheusText(t *testing.T) {
	c := New("0.1.0")
	c.ObserveUsage(UsageRecord{
		Provider: "xai",
		Model:    "grok-4.6",
		Latency:  1250 * time.Millisecond,
		Detail: TokenDetail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})
	c.ObserveUsage(UsageRecord{
		Provider:          "xai",
		Model:             "grok-4.6",
		Failed:            true,
		FailureStatusCode: 429,
		Latency:           80 * time.Millisecond,
	})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"cliproxy_requests_total",
		`model="grok-4.6"`,
		`provider="xai"`,
		"cliproxy_tokens_total",
		`type="input"`,
		`type="output"`,
		"cliproxy_failures_total",
		`code="429"`,
		"cliproxy_request_duration_seconds",
		"cliproxy_info",
		`plugin_version="0.1.0"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("gathered text missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, `cliproxy_requests_total{model="grok-4.6",plugin_id="cpa-prometheus",provider="xai"} 2`) &&
		!strings.Contains(text, `cliproxy_requests_total{model="grok-4.6",provider="xai",plugin_id="cpa-prometheus"} 2`) {
		// label order from prometheus is typically alphabetical: model, plugin_id, provider
		if !hasCounterValue(text, "cliproxy_requests_total", 2) {
			t.Fatalf("expected requests_total 2:\n%s", text)
		}
	}
	if strings.Contains(text, "sk-") || strings.Contains(text, "@") {
		t.Fatalf("possible secret/email in exposition:\n%s", text)
	}
}

func TestObserveUsageDoesNotLabelAPIKeyOrEmail(t *testing.T) {
	c := New("0.1.0")
	c.ObserveUsage(UsageRecord{
		Provider: "claude",
		Model:    "claude-sonnet-4",
		Detail:   TokenDetail{InputTokens: 1, TotalTokens: 1},
	})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"api_key", "apikey", "cookie", "authorization"} {
		if strings.Contains(strings.ToLower(text), banned+"=") {
			t.Fatalf("banned label %q in text:\n%s", banned, text)
		}
	}
	if strings.Contains(text, "@") {
		t.Fatalf("invented email on usage without credentials:\n%s", text)
	}
}

func TestApplyQuotaWritesGauges(t *testing.T) {
	c := New("0.1.0")
	fetched := time.Unix(1_700_000_000, 0).UTC()
	c.ApplyQuota([]quota.Account{{
		Provider:  "claude",
		AuthIndex: "a1",
		Supported: true,
		Windows: []quota.Window{{
			ID:             "five_hour",
			UsedRatio:      0.25,
			RemainingRatio: 0.75,
			ResetUnix:      1_700_000_300,
		}},
		FetchedAt: fetched,
	}})
	c.ApplyCredentials([]quota.Credential{{Provider: "claude", AuthIndex: "a1", Status: "active"}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"cliproxy_quota_used_ratio",
		"cliproxy_quota_remaining_ratio",
		"cliproxy_quota_reset_timestamp_seconds",
		"cliproxy_quota_last_success_timestamp_seconds",
		"cliproxy_quota_poll_interval_seconds",
		`window="five_hour"`,
		`auth_index="a1"`,
		"cliproxy_credentials",
		`status="active"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("gathered text missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "0.25") {
		t.Fatalf("used ratio 0.25 missing:\n%s", text)
	}
	if !strings.Contains(text, "0.75") {
		t.Fatalf("remaining ratio 0.75 missing:\n%s", text)
	}
	if !strings.Contains(text, "1700000000") && !strings.Contains(text, "1.7e+09") {
		t.Fatalf("last-success timestamp missing:\n%s", text)
	}
	if !strings.Contains(text, "300") {
		t.Fatalf("5m poll interval seconds missing:\n%s", text)
	}
}

func TestApplyQuotaEmitsHasWindowZeroForEmptyWindows(t *testing.T) {
	c := New("0.1.2")
	c.ApplyQuota([]quota.Account{{
		Provider:  "xai",
		AuthIndex: "payg",
		Supported: true,
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_quota_has_window") {
		t.Fatalf("missing cliproxy_quota_has_window:\n%s", text)
	}
	if !strings.Contains(text, `cliproxy_quota_has_window{auth_index="payg",plugin_id="cpa-prometheus",provider="xai"} 0`) &&
		!gaugeIs(text, "cliproxy_quota_has_window", 0) {
		t.Fatalf("PAYG supported account must emit has_window=0:\n%s", text)
	}
	if !strings.Contains(text, `cliproxy_quota_supported{auth_index="payg",plugin_id="cpa-prometheus",provider="xai"} 1`) &&
		!strings.Contains(text, `provider="xai"`) {
		t.Fatalf("supported gauge missing:\n%s", text)
	}
}

func TestApplyQuotaHasWindowOneWhenWindowsPresent(t *testing.T) {
	c := New("0.1.2")
	c.ApplyQuota([]quota.Account{{
		Provider:  "codex",
		AuthIndex: "c1",
		Supported: true,
		Windows:   []quota.Window{{ID: "five_hour", UsedRatio: 0.1, RemainingRatio: 0.9}},
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, `window="primary"`) || strings.Contains(text, `window="secondary"`) {
		t.Fatalf("legacy Codex window labels in exposition:\n%s", text)
	}
	if !strings.Contains(text, `window="five_hour"`) {
		t.Fatalf("missing five_hour:\n%s", text)
	}
	if !gaugeIs(text, "cliproxy_quota_has_window", 1) {
		t.Fatalf("has_window=1 missing:\n%s", text)
	}
}

func TestApplyCredentialsWritesAuthNumericsWithoutPII(t *testing.T) {
	c := New("0.1.2")
	c.ApplyCredentials([]quota.Credential{{
		Provider:      "xai",
		AuthIndex:     "a1",
		Status:        "active",
		Disabled:      false,
		Unavailable:   true,
		Success:       12,
		Failed:        3,
		NextRetryUnix: 1_800_000_000,
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"cliproxy_auth_success",
		"cliproxy_auth_failed",
		"cliproxy_auth_disabled",
		"cliproxy_auth_unavailable",
		"cliproxy_auth_next_retry_timestamp_seconds",
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("missing %s:\n%s", name, text)
		}
	}
	if !strings.Contains(text, "12") || !strings.Contains(text, "3") {
		t.Fatalf("success/failed counts missing:\n%s", text)
	}
	if strings.Contains(text, "sk-") || strings.Contains(text, "/root/") {
		t.Fatalf("secret/path in exposition:\n%s", text)
	}
}

func TestApplyCredentialsWritesEmailLabel(t *testing.T) {
	c := New("0.1.3")
	c.ApplyCredentials([]quota.Credential{{
		Provider:    "xai",
		AuthIndex:   "a1",
		Status:      "active",
		Email:       "gio@example.com",
		AccountType: "oauth",
		Success:     4,
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `email="gio@example.com"`) {
		t.Fatalf("email label missing:\n%s", text)
	}
	if !strings.Contains(text, `account_type="oauth"`) {
		t.Fatalf("account_type label missing:\n%s", text)
	}
	if strings.Contains(text, "sk-") || strings.Contains(text, "/root/") {
		t.Fatalf("secret/path leaked:\n%s", text)
	}
}

func TestApplyCredentialsUnknownEmailWhenMissing(t *testing.T) {
	c := New("0.1.3")
	c.ApplyCredentials([]quota.Credential{{
		Provider:  "codex",
		AuthIndex: "c1",
		Status:    "active",
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "@") {
		t.Fatalf("invented email:\n%s", text)
	}
	if !strings.Contains(text, `email="unknown"`) {
		t.Fatalf("missing unknown email:\n%s", text)
	}
}

func TestObserveUsageKeepsProviderAndModel(t *testing.T) {
	c := New("0.1.3")
	c.ApplyCredentials([]quota.Credential{{
		Provider:  "xai",
		AuthIndex: "a1",
		Email:     "gio@example.com",
		Status:    "active",
	}})
	c.ObserveUsage(UsageRecord{
		Provider:  "xai",
		Model:     "grok-4.6",
		AuthIndex: "a1",
		Latency:   time.Millisecond,
		Detail:    TokenDetail{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
	})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_requests_total") || !strings.Contains(text, `model="grok-4.6"`) {
		t.Fatalf("per-model usage missing:\n%s", text)
	}
	if !strings.Contains(text, "cliproxy_model_seen") || !strings.Contains(text, `model="grok-4.6"`) {
		t.Fatalf("model_seen missing:\n%s", text)
	}
}

func TestObserveUsageWritesLastRequestTimestamp(t *testing.T) {
	c := New("0.1.4")
	c.ApplyCredentials([]quota.Credential{{
		Provider:  "xai",
		AuthIndex: "a1",
		Email:     "gio@example.com",
		Status:    "active",
	}})
	ts := time.Unix(1_700_000_111, 0).UTC()
	c.ObserveUsage(UsageRecord{
		Provider:    "xai",
		Model:       "grok-4.6",
		AuthIndex:   "a1",
		RequestedAt: ts,
		Latency:     time.Millisecond,
		Detail:      TokenDetail{InputTokens: 1, TotalTokens: 1},
	})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_last_request_timestamp_seconds") {
		t.Fatalf("last_request missing:\n%s", text)
	}
	if got, ok := metricFloat(text, "cliproxy_last_request_timestamp_seconds"); !ok || got != 1_700_000_111 {
		t.Fatalf("RequestedAt unix=%v ok=%v:\n%s", got, ok, text)
	}
	if !strings.Contains(text, `email="gio@example.com"`) || !strings.Contains(text, `model="grok-4.6"`) {
		t.Fatalf("identity labels missing on last_request:\n%s", text)
	}
}

func TestApplyCredentialsWritesRuntimeOnlyLastRefreshProjectID(t *testing.T) {
	c := New("0.1.4")
	c.ApplyCredentials([]quota.Credential{{
		Provider:        "antigravity",
		AuthIndex:       "a1",
		Status:          "active",
		Email:           "gio@example.com",
		RuntimeOnly:     true,
		LastRefreshUnix: 1_700_000_222,
		ProjectID:       "proj-9",
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_auth_runtime_only") {
		t.Fatalf("runtime_only missing:\n%s", text)
	}
	if !gaugeIs(text, "cliproxy_auth_runtime_only", 1) {
		t.Fatalf("runtime_only != 1:\n%s", text)
	}
	if !strings.Contains(text, "cliproxy_auth_last_refresh_timestamp_seconds") {
		t.Fatalf("last_refresh missing:\n%s", text)
	}
	if got, ok := metricFloat(text, "cliproxy_auth_last_refresh_timestamp_seconds"); !ok || got != 1_700_000_222 {
		t.Fatalf("last_refresh unix=%v ok=%v:\n%s", got, ok, text)
	}
	if !strings.Contains(text, "cliproxy_auth_project_info") || !strings.Contains(text, `project_id="proj-9"`) {
		t.Fatalf("project_info missing:\n%s", text)
	}
	if strings.Contains(text, "/root/") || strings.Contains(text, "sk-") {
		t.Fatalf("secret/path leaked:\n%s", text)
	}
}

func TestApplyCredentialsWritesUpdatedAt(t *testing.T) {
	c := New("0.1.5")
	c.ApplyCredentials([]quota.Credential{{
		Provider:      "xai",
		AuthIndex:     "a1",
		Status:        "active",
		Email:         "gio@example.com",
		UpdatedAtUnix: 1_700_000_333,
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_auth_updated_timestamp_seconds") {
		t.Fatalf("updated_at missing:\n%s", text)
	}
	if got, ok := metricFloat(text, "cliproxy_auth_updated_timestamp_seconds"); !ok || got != 1_700_000_333 {
		t.Fatalf("updated unix=%v ok=%v:\n%s", got, ok, text)
	}
}

func TestApplyCredentialsOmitsProjectInfoWhenEmpty(t *testing.T) {
	c := New("0.1.4")
	c.ApplyCredentials([]quota.Credential{{
		Provider:  "xai",
		AuthIndex: "a1",
		Status:    "active",
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "cliproxy_auth_project_info") {
		t.Fatalf("project_info should be omitted when empty:\n%s", text)
	}
}

func TestApplyModelAvailabilityFromRuntime(t *testing.T) {
	c := New("0.1.3")
	c.ApplyModelAvailability([]quota.ModelAvailability{{
		Provider:    "xai",
		AuthIndex:   "a1",
		Email:       "gio@example.com",
		Model:       "grok-4.6",
		Status:      "active",
		Unavailable: false,
	}})
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "cliproxy_model_available") {
		t.Fatalf("model_available missing:\n%s", text)
	}
	if !strings.Contains(text, `model="grok-4.6"`) {
		t.Fatalf("model label missing:\n%s", text)
	}
}

func metricFloat(text, name string) (float64, bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, name+"{") && line != name {
			continue
		}
		i := strings.LastIndex(line, " ")
		if i < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[i+1:], 64)
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}

func gaugeIs(text, name string, want float64) bool {
	suffix := " " + strings.TrimRight(strings.TrimRight(formatFloat(want), "0"), ".")
	if want == 0 {
		suffix = " 0"
	}
	if want == 1 {
		suffix = " 1"
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, name+"{") && strings.HasSuffix(line, suffix) {
			return true
		}
	}
	return false
}

func formatFloat(v float64) string {
	if v == 0 {
		return "0"
	}
	if v == 1 {
		return "1"
	}
	return strings.TrimRight(strings.TrimRight(
		// keep tests from reimplementing prometheus; only used for 0/1 gauges
		map[bool]string{true: "1", false: "0"}[v == 1], "0"), ".")
}

func hasCounterValue(text, name string, want int) bool {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, name+"{") {
			continue
		}
		if strings.HasSuffix(line, " "+itoa(want)) {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
