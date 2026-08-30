package collector

import (
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
	for _, banned := range []string{"api_key", "apikey", "email", "cookie", "authorization"} {
		if strings.Contains(strings.ToLower(text), banned+"=") {
			t.Fatalf("banned label %q in text:\n%s", banned, text)
		}
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
