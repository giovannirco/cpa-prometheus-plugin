package quota

import (
	"math"
	"testing"
	"time"
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
	if got[0].ID != "five_hour" || got[0].UsedRatio != 0.4 || got[0].RemainingRatio != 0.6 {
		t.Fatalf("five_hour = %#v", got[0])
	}
	if got[1].ID != "seven_day" || got[1].UsedRatio != 0.1 || got[1].RemainingRatio != 0.9 {
		t.Fatalf("seven_day = %#v", got[1])
	}
}

func TestParseCodexAliasesPrimarySecondaryToFiveHourSevenDay(t *testing.T) {
	body := []byte(`{"rate_limit":{"primary_window":{"used_percent":100},"secondary_window":{"used_percent":0}}}`)
	got := ParseWindows("codex", body)
	ids := windowIDs(got)
	if contains(ids, "primary") || contains(ids, "secondary") {
		t.Fatalf("legacy Codex window ids leaked: %#v", got)
	}
	if !contains(ids, "five_hour") || !contains(ids, "seven_day") {
		t.Fatalf("want five_hour and seven_day, got %#v", got)
	}
}

func TestParseAntigravityRemainingFraction(t *testing.T) {
	body := []byte(`{"models":[{"name":"gemini-2.5-pro","quotaInfo":{"remainingFraction":0.7,"resetTime":"2026-08-30T12:00:00Z"}}]}`)
	got := ParseWindows("antigravity", body)
	if len(got) != 1 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if got[0].ID != "gemini_weekly" {
		t.Fatalf("collapsed id = %q, want gemini_weekly", got[0].ID)
	}
	if math.Abs(got[0].RemainingRatio-0.7) > 1e-9 || math.Abs(got[0].UsedRatio-0.3) > 1e-9 {
		t.Fatalf("got %#v", got[0])
	}
}

func TestParseAntigravityCollapsesModelsIntoUIGroups(t *testing.T) {
	body := []byte(`{"models":[
		{"name":"gemini-2.5-pro","quotaInfo":{"remainingFraction":0.9,"resetTime":"2026-09-01T00:00:00Z"}},
		{"name":"gemini-3-flash","quotaInfo":{"remainingFraction":0.4,"resetTime":"2026-09-01T00:00:00Z"}},
		{"name":"chat_20706","quotaInfo":{"remainingFraction":0.8,"resetTime":"2026-09-01T00:00:00Z"}},
		{"name":"tab_flash_lite_preview","quotaInfo":{"remainingFraction":1,"resetTime":"2026-09-01T00:00:00Z"}},
		{"name":"claude-sonnet-4-6","quotaInfo":{"remainingFraction":0.5,"resetTime":"2026-09-02T00:00:00Z"}},
		{"name":"claude-opus-4-6-thinking","quotaInfo":{"remainingFraction":0.2,"resetTime":"2026-09-02T00:00:00Z"}},
		{"name":"gpt-oss-120b-medium","quotaInfo":{"remainingFraction":0.6,"resetTime":"2026-09-02T00:00:00Z"}}
	]}`)
	got := ParseWindows("antigravity", body)
	ids := windowIDs(got)
	if len(got) != 2 {
		t.Fatalf("want 2 UI groups, got %d %#v", len(got), got)
	}
	if contains(ids, "gemini-2.5-pro") || contains(ids, "claude-sonnet-4-6") || contains(ids, "gpt-oss-120b-medium") {
		t.Fatalf("per-model windows leaked: %#v", got)
	}
	gemini := byID(got, "gemini_weekly")
	claudeGPT := byID(got, "claude_gpt_weekly")
	if gemini == nil || claudeGPT == nil {
		t.Fatalf("missing UI group ids: %#v", got)
	}
	if math.Abs(gemini.RemainingRatio-0.4) > 1e-9 {
		t.Fatalf("gemini group should use tightest remaining 0.4, got %#v", gemini)
	}
	if math.Abs(claudeGPT.RemainingRatio-0.2) > 1e-9 {
		t.Fatalf("claude/gpt group should use tightest remaining 0.2, got %#v", claudeGPT)
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

func TestParseXAICreditUsagePercent(t *testing.T) {
	body := []byte(`{"config":{"creditUsagePercent":8,"currentPeriod":{"end":"2026-09-02T07:52:00Z"}}}`)
	got := ParseWindows("xai", body)
	if len(got) != 1 {
		t.Fatalf("len=%d %#v", len(got), got)
	}
	if got[0].ID != "weekly" || math.Abs(got[0].UsedRatio-0.08) > 1e-9 || math.Abs(got[0].RemainingRatio-0.92) > 1e-9 {
		t.Fatalf("got %#v", got[0])
	}
	if got[0].ResetUnix == 0 {
		t.Fatal("missing reset")
	}
}

func TestParseDoesNotRequireTokensInBody(t *testing.T) {
	got := ParseWindows("claude", []byte(`{"five_hour":{"utilization":0}}`))
	if len(got) != 1 {
		t.Fatalf("%#v", got)
	}
}

func TestParseXAIGrokBuildWindowOnlyWhenPresent(t *testing.T) {
	without := []byte(`{"config":{"creditUsagePercent":8,"currentPeriod":{"end":"2026-09-02T07:52:00Z"}}}`)
	got := ParseWindows("xai", without)
	if contains(windowIDs(got), "grok_build") || contains(windowIDs(got), "grokbuild") {
		t.Fatalf("invented grok_build: %#v", got)
	}
	with := []byte(`{"config":{"creditUsagePercent":8,"currentPeriod":{"end":"2026-09-02T07:52:00Z"},"grokBuild":{"used_percent":12,"resets_at":"2026-09-05T00:00:00Z"}}}`)
	got = ParseWindows("xai", with)
	gb := byID(got, "grok_build")
	if gb == nil {
		t.Fatalf("missing grok_build when JSON has grokBuild: %#v", got)
	}
	if math.Abs(gb.UsedRatio-0.12) > 1e-9 {
		t.Fatalf("grok_build used %#v", gb)
	}
	if byID(got, "weekly") == nil {
		t.Fatalf("weekly should remain: %#v", got)
	}
}

func TestParseXAIPayAsYouGoHasNoWindows(t *testing.T) {
	got := ParseWindows("xai", []byte(`{"config":{}}`))
	if len(got) != 0 {
		t.Fatalf("PAYG should not invent windows: %#v", got)
	}
}

func TestParseCodexResetCreditsFromUsage(t *testing.T) {
	body := []byte(`{
		"rate_limit":{"primary_window":{"used_percent":40,"reset_after":1700000400},"secondary_window":{"used_percent":10,"reset_after":1700000800}},
		"rate_limit_reset_credits":{"available_count":3,"credits":[
			{"status":"available","expires_at":"2026-09-12T01:00:00Z"},
			{"status":"available","expires_at":"2026-09-18T02:00:00Z"},
			{"status":"redeemed","expires_at":"2026-08-01T00:00:00Z"}
		]}
	}`)
	got := ParseWindows("codex", body)
	if len(got) != 2 {
		t.Fatalf("windows %#v", got)
	}
	credits, ok := ParseResetCredits("codex", body)
	if !ok || credits.Available != 3 {
		t.Fatalf("reset credits = %#v ok=%v, want available 3", credits, ok)
	}
	wantExp, err := time.Parse(time.RFC3339, "2026-09-12T01:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if credits.ExpiresUnix != wantExp.Unix() {
		t.Fatalf("earliest available expiry = %d, want %d", credits.ExpiresUnix, wantExp.Unix())
	}
}

func TestParseCodexResetCreditsDedicatedBody(t *testing.T) {
	body := []byte(`{"available_count":1,"credits":[{"status":"available","expires_at":"2026-10-01T00:00:00Z"}]}`)
	credits, ok := ParseResetCredits("codex", body)
	if !ok || credits.Available != 1 {
		t.Fatalf("got %#v ok=%v", credits, ok)
	}
}

func TestParseResetCreditsZeroIsPresent(t *testing.T) {
	body := []byte(`{"rate_limit_reset_credits":{"available_count":0}}`)
	credits, ok := ParseResetCredits("codex", body)
	if !ok || credits.Available != 0 {
		t.Fatalf("zero must still be present: %#v ok=%v", credits, ok)
	}
}

func TestParseResetCreditsAbsentForXAI(t *testing.T) {
	_, ok := ParseResetCredits("xai", []byte(`{"config":{"creditUsagePercent":8}}`))
	if ok {
		t.Fatal("xAI billing JSON has no banked reset credits")
	}
}

func TestParseGeminiCLIBuckets(t *testing.T) {
	body := []byte(`{"buckets":[{"modelId":"gemini-2.5-pro","remainingFraction":0.4,"resetTime":"2026-09-01T00:00:00Z"}]}`)
	got := ParseWindows("gemini-cli", body)
	if len(got) != 1 || got[0].ID != "gemini-2_5-pro" {
		t.Fatalf("gemini-cli windows %#v", got)
	}
	if math.Abs(got[0].RemainingRatio-0.4) > 1e-9 {
		t.Fatalf("remaining %#v", got[0])
	}
}

func windowIDs(windows []Window) []string {
	out := make([]string, 0, len(windows))
	for _, w := range windows {
		out = append(out, w.ID)
	}
	return out
}

func byID(windows []Window, id string) *Window {
	for i := range windows {
		if windows[i].ID == id {
			return &windows[i]
		}
	}
	return nil
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
