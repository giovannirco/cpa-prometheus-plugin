package quota

import (
	"math"
	"testing"
)

func TestParseClaudeWindows(t *testing.T) {
	body := []byte(`{"five_hour":{"utilization":0.2,"resets_at":"2026-08-30T01:00:00Z"},"seven_day":{"utilization":0.5,"resets_at":"2026-09-01T00:00:00Z"}}`)
	got := ParseWindows("claude", body)
	if len(got) != 2 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if got[0].ID != "five_hour" || got[0].UsedRatio != 0.2 || got[0].RemainingRatio != 0.8 {
		t.Fatalf("five_hour = %#v", got[0])
	}
	if got[0].ResetUnix == 0 {
		t.Fatal("missing reset")
	}
}

func TestParseCodexUsedPercent(t *testing.T) {
	body := []byte(`{"rate_limit":{"primary_window":{"used_percent":40,"reset_after":1700000400},"secondary_window":{"used_percent":10,"reset_after":1700000800}}}`)
	got := ParseWindows("codex", body)
	if len(got) != 2 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if got[0].ID != "primary" || got[0].UsedRatio != 0.4 || got[0].RemainingRatio != 0.6 {
		t.Fatalf("primary = %#v", got[0])
	}
}

func TestParseAntigravityRemainingFraction(t *testing.T) {
	body := []byte(`{"models":[{"name":"gemini-2.5-pro","quotaInfo":{"remainingFraction":0.7,"resetTime":"2026-08-30T12:00:00Z"}}]}`)
	got := ParseWindows("antigravity", body)
	if len(got) != 1 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if math.Abs(got[0].RemainingRatio-0.7) > 1e-9 || math.Abs(got[0].UsedRatio-0.3) > 1e-9 {
		t.Fatalf("got %#v", got[0])
	}
}

func TestParseKimiLimits(t *testing.T) {
	body := []byte(`{"limits":[{"type":"five_hour","used":20,"limit":100,"resets_at":"2026-08-30T06:00:00Z"},{"type":"weekly","used":10,"limit":50,"resets_at":"2026-09-05T00:00:00Z"}]}`)
	got := ParseWindows("kimi", body)
	if len(got) != 2 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if got[0].UsedRatio != 0.2 || got[0].RemainingRatio != 0.8 {
		t.Fatalf("five_hour %#v", got[0])
	}
}

func TestParseXAICredits(t *testing.T) {
	body := []byte(`{"credits":{"used":25,"limit":100,"resets_at":"2026-09-01T00:00:00Z"}}`)
	got := ParseWindows("xai", body)
	if len(got) != 1 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if got[0].ID != "credits" || got[0].UsedRatio != 0.25 || got[0].RemainingRatio != 0.75 {
		t.Fatalf("got %#v", got[0])
	}
}

func TestParseDoesNotRequireTokensInBody(t *testing.T) {
	got := ParseWindows("claude", []byte(`{"five_hour":{"utilization":0}}`))
	if len(got) != 1 {
		t.Fatalf("%#v", got)
	}
}
