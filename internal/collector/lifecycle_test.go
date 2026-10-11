package collector

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/labels"
)

var t0 = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func completion(id, outcome string, code int, d time.Duration) CompletionRecord {
	return CompletionRecord{
		RequestID: id, SourceFormat: "openai", RequestedModel: "claude-opus-5-5",
		Stream: true, Outcome: outcome, StatusCode: code,
		StartedAt: t0, CompletedAt: t0.Add(d),
	}
}

func TestCompletionCountsOutcomeCodeAndLatency(t *testing.T) {
	c := New("0.1.0")
	c.ObserveCompletion(completion("r1", "succeeded", 200, 45*time.Second))
	c.ObserveCompletion(completion("r2", "failed", 429, time.Second))
	c.ObserveCompletion(completion("r3", "canceled", 0, 2*time.Second))

	m := c.lifecycle
	for _, tc := range []struct {
		outcome, code string
	}{{"succeeded", "200"}, {"failed", "429"}, {"canceled", "none"}} {
		got := testutil.ToFloat64(m.requests.WithLabelValues("openai", "claude-opus-5-5", "true", tc.outcome, tc.code))
		if got != 1 {
			t.Fatalf("requests{outcome=%s,code=%s} = %v, want 1", tc.outcome, tc.code, got)
		}
	}
	text, err := c.Gather()
	if err != nil {
		t.Fatal(err)
	}
	// A 45s request must land in a finite bucket: DefBuckets would put it in +Inf.
	if !strings.Contains(text, `cliproxy_client_request_duration_seconds_bucket{format="openai",model="claude-opus-5-5",outcome="succeeded",plugin_id="cpa-prometheus",stream="true",le="30"} 0`) && strings.Contains(text, `cliproxy_client_request_duration_seconds_bucket{format="openai",model="claude-opus-5-5",outcome="succeeded",plugin_id="cpa-prometheus",stream="true",le="60"} 1`) {
		t.Fatalf("45s request not in the 60s bucket:\n%s", text)
	}
}

func TestInFlightAndAttempts(t *testing.T) {
	c := New("0.1.0")
	m := c.lifecycle
	c.AdmitRequest("r1", "claude")
	c.AdmitRequest("r1", "claude") // duplicate admission is idempotent
	c.AdmitRequest("r2", "claude")
	if got := testutil.ToFloat64(m.inFlight.WithLabelValues("claude")); got != 2 {
		t.Fatalf("in flight = %v, want 2", got)
	}
	c.RecordAttempt("r1")
	c.RecordAttempt("r1")
	c.RecordAttempt("r1")
	c.RecordAttempt("unknown") // attempts for untracked requests are ignored

	rec := completion("r1", "succeeded", 200, time.Second)
	rec.SourceFormat = "claude"
	c.ObserveCompletion(rec)
	c.ObserveCompletion(rec) // duplicate terminal event must not double-decrement
	if got := testutil.ToFloat64(m.inFlight.WithLabelValues("claude")); got != 1 {
		t.Fatalf("in flight after completion = %v, want 1", got)
	}
	text, _ := c.Gather()
	if !strings.Contains(text, `cliproxy_client_request_attempts_sum{format="claude",model="claude-opus-5-5",outcome="succeeded",plugin_id="cpa-prometheus"} 3`) {
		t.Fatalf("attempts not recorded as 3:\n%s", text)
	}
}

func TestCompletionWithoutAdmissionLeavesInFlightAlone(t *testing.T) {
	c := New("0.1.0")
	c.ObserveCompletion(completion("never-admitted", "succeeded", 200, time.Second))
	if got := testutil.ToFloat64(c.lifecycle.inFlight.WithLabelValues("openai")); got != 0 {
		t.Fatalf("in flight = %v, want 0", got)
	}
	if n := testutil.CollectAndCount(c.lifecycle.attempts); n != 0 {
		t.Fatalf("attempts recorded for an untracked request: %d series", n)
	}
}

func TestStaleRequestsAreSwept(t *testing.T) {
	c := New("0.1.0")
	m := c.lifecycle
	now := t0
	m.now = func() time.Time { return now }
	c.AdmitRequest("lost", "openai")
	now = now.Add(staleRequestAge + time.Minute)
	for i := 1; i < staleSweepEvery; i++ {
		id := "r" + strconv.Itoa(i)
		c.AdmitRequest(id, "openai")
		c.ObserveCompletion(CompletionRecord{RequestID: id, SourceFormat: "openai", Outcome: "succeeded"})
	}
	if got := testutil.ToFloat64(m.inFlight.WithLabelValues("openai")); got != 0 {
		t.Fatalf("in flight = %v after sweep, want 0", got)
	}
}

func TestClientModelCardinalityIsBounded(t *testing.T) {
	c := New("0.1.0")
	for i := 0; i < maxClientModels+50; i++ {
		rec := completion("r", "failed", 404, time.Second)
		rec.RequestedModel = "random-model-" + strconv.Itoa(i)
		c.ObserveCompletion(rec)
	}
	if n := testutil.CollectAndCount(c.lifecycle.requests); n != maxClientModels+1 {
		t.Fatalf("request series = %d, want %d (cap plus overflow)", n, maxClientModels+1)
	}
	got := testutil.ToFloat64(c.lifecycle.requests.WithLabelValues("openai", labels.Overflow, "true", "failed", "404"))
	if got != 50 {
		t.Fatalf("overflow series = %v, want 50", got)
	}
}

func TestStreamTimingFromUsage(t *testing.T) {
	c := New("0.1.0")
	c.ObserveUsage(UsageRecord{
		Provider: "claude", Model: "claude-opus-5-5", Stream: true,
		TTFT: 800 * time.Millisecond, Latency: 10800 * time.Millisecond,
		Detail: TokenDetail{OutputTokens: 500},
	})
	// Non-streaming, failed, and TTFT-less records must not be observed.
	c.ObserveUsage(UsageRecord{Provider: "claude", Model: "claude-opus-5-5", TTFT: time.Second, Latency: 2 * time.Second})
	c.ObserveUsage(UsageRecord{Provider: "claude", Model: "claude-opus-5-5", Stream: true, Failed: true, TTFT: time.Second})
	c.ObserveUsage(UsageRecord{Provider: "claude", Model: "claude-opus-5-5", Stream: true, Latency: time.Second})

	text, _ := c.Gather()
	for _, want := range []string{
		`cliproxy_ttft_seconds_count{model="claude-opus-5-5",plugin_id="cpa-prometheus",provider="claude"} 1`,
		`cliproxy_ttft_seconds_sum{model="claude-opus-5-5",plugin_id="cpa-prometheus",provider="claude"} 0.8`,
		// 500 tokens over 10s of generation after the first token.
		`cliproxy_output_tokens_per_second_sum{model="claude-opus-5-5",plugin_id="cpa-prometheus",provider="claude"} 50`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

// Regression: CPA completes GET /v1/models through request.complete with no model.
func TestModelListCompletionIsNotCounted(t *testing.T) {
	c := New("0.1.0")
	c.AdmitRequest("models", "openai")
	c.ObserveCompletion(CompletionRecord{RequestID: "models", SourceFormat: "openai", Outcome: "succeeded", StatusCode: 200})
	if n := testutil.CollectAndCount(c.lifecycle.requests); n != 0 {
		t.Fatalf("model-list completion produced %d request series", n)
	}
	if got := testutil.ToFloat64(c.lifecycle.inFlight.WithLabelValues("openai")); got != 0 {
		t.Fatalf("in flight = %v, want 0", got)
	}
}
