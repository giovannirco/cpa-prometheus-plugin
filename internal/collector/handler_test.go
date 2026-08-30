package collector

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

func TestMetricsHandlerPrometheusText(t *testing.T) {
	c := New("0.1.0")
	c.SetPublicMetrics(true)
	c.ObserveUsage(UsageRecord{
		Provider: "codex",
		Model:    "gpt-5.5",
		Latency:  2 * time.Second,
		Detail:   TokenDetail{InputTokens: 3, OutputTokens: 7, TotalTokens: 10},
	})
	c.ApplyQuota([]quota.Account{{
		Provider:  "codex",
		AuthIndex: "idx-1",
		Supported: true,
		Windows: []quota.Window{{
			ID:             "primary",
			UsedRatio:      0.1,
			RemainingRatio: 0.9,
			ResetUnix:      1_800_000_000,
		}},
		FetchedAt: time.Unix(1_800_000_000, 0).UTC(),
	}})
	c.ApplyCredentials([]quota.Credential{{Provider: "codex", Status: "active"}})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	c.MetricsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want prometheus text", ct)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, name := range []string{
		"cliproxy_info",
		"cliproxy_up",
		"cliproxy_credentials",
		"cliproxy_models_seen",
		"cliproxy_requests_total",
		"cliproxy_tokens_total",
		"cliproxy_request_duration_seconds",
		"cliproxy_quota_used_ratio",
		"cliproxy_quota_remaining_ratio",
		"cliproxy_quota_reset_timestamp_seconds",
		"cliproxy_quota_last_success_timestamp_seconds",
		"cliproxy_quota_poll_interval_seconds",
		"cliproxy_last_request_timestamp_seconds",
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("handler body missing %s:\n%s", name, text)
		}
	}
}

func TestMetricsHandlerDeniesAnonymousWhenNotPublic(t *testing.T) {
	c := New("0.1.0")
	rec := httptest.NewRecorder()
	c.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

func TestMetricsHandlerHonorsScrapeToken(t *testing.T) {
	c := New("0.1.0")
	c.SetScrapeToken("s3cret")
	rec := httptest.NewRecorder()
	c.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	c.MetricsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	bad := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	bad.Header.Set("Authorization", "Bearer wrong")
	c.MetricsHandler().ServeHTTP(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	lower := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	lower.Header.Set("Authorization", "bearer s3cret")
	c.MetricsHandler().ServeHTTP(rec, lower)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer prefix should be case-insensitive, status %d", rec.Code)
	}
}
