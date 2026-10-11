package collector

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/labels"
)

// llmLatencyBuckets covers end-to-end LLM requests, which routinely run tens of
// seconds to minutes; prometheus.DefBuckets stops at 10s.
var llmLatencyBuckets = []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300, 600}

// ttftBuckets covers time to first token for streaming responses.
var ttftBuckets = []float64{0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 3, 5, 10, 20, 30}

// outputSpeedBuckets covers output tokens per second after the first token.
var outputSpeedBuckets = []float64{5, 10, 20, 30, 40, 60, 80, 100, 150, 200, 300, 500}

// attemptBuckets covers credential attempts per client request.
var attemptBuckets = []float64{1, 2, 3, 4, 5, 8, 13}

const (
	// maxClientModels caps distinct client-requested model label values. Clients
	// choose this string freely, so it must not create unbounded series.
	maxClientModels = 256
	// staleRequestAge bounds how long an admitted request is tracked without a
	// terminal event before it stops counting as in flight.
	staleRequestAge = 2 * time.Hour
	// staleSweepEvery is how many admissions pass between stale sweeps.
	staleSweepEvery = 1024
)

// CompletionRecord is the terminal event CPA emits for every client request.
type CompletionRecord struct {
	RequestID      string
	SourceFormat   string
	RequestedModel string
	Model          string
	Stream         bool
	Outcome        string
	StatusCode     int
	StartedAt      time.Time
	CompletedAt    time.Time
}

type trackedRequest struct {
	format   string
	attempts int
	admitted time.Time
}

// lifecycleMetrics holds the client-side metrics fed by request.complete and,
// when enabled, by the request interceptor.
type lifecycleMetrics struct {
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	inFlight  *prometheus.GaugeVec
	attempts  *prometheus.HistogramVec
	ttft      *prometheus.HistogramVec
	speed     *prometheus.HistogramVec
	modelsCap *labels.Limiter

	mu         sync.Mutex
	active     map[string]*trackedRequest
	admissions int
	now        func() time.Time
}

func newLifecycleMetrics(reg prometheus.Registerer, constLabels prometheus.Labels) *lifecycleMetrics {
	m := &lifecycleMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "cliproxy_client_requests_total",
			Help:        "Client requests by terminal outcome (succeeded, failed, rejected, canceled) from request.complete.",
			ConstLabels: constLabels,
		}, []string{"format", "model", "stream", "outcome", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "cliproxy_client_request_duration_seconds",
			Help:        "End-to-end client request latency including credential retries, from request.complete.",
			ConstLabels: constLabels,
			Buckets:     llmLatencyBuckets,
		}, []string{"format", "model", "stream", "outcome"}),
		inFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "cliproxy_client_requests_in_flight",
			Help:        "Client requests admitted but not yet completed. Requires track-in-flight.",
			ConstLabels: constLabels,
		}, []string{"format"}),
		attempts: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "cliproxy_client_request_attempts",
			Help:        "Credential attempts per completed client request; above 1 means CPA retried or switched credentials. Requires track-in-flight.",
			ConstLabels: constLabels,
			Buckets:     attemptBuckets,
		}, []string{"format", "model", "outcome"}),
		ttft: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "cliproxy_ttft_seconds",
			Help:        "Time to first token of successful streaming upstream requests, from usage.handle.",
			ConstLabels: constLabels,
			Buckets:     ttftBuckets,
		}, []string{"provider", "model"}),
		speed: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "cliproxy_output_tokens_per_second",
			Help:        "Output tokens per second after the first token, for successful streaming upstream requests.",
			ConstLabels: constLabels,
			Buckets:     outputSpeedBuckets,
		}, []string{"provider", "model"}),
		modelsCap: labels.NewLimiter(maxClientModels),
		active:    map[string]*trackedRequest{},
		now:       time.Now,
	}
	reg.MustRegister(m.requests, m.duration, m.inFlight, m.attempts, m.ttft, m.speed)
	return m
}

// AdmitRequest marks a client request as in flight (request.intercept_before).
func (c *Collector) AdmitRequest(requestID, sourceFormat string) {
	m := c.lifecycle
	if requestID == "" {
		return
	}
	format := labels.Format(sourceFormat)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.active[requestID]; exists {
		return
	}
	m.active[requestID] = &trackedRequest{format: format, admitted: m.now()}
	m.inFlight.WithLabelValues(format).Inc()
	m.admissions++
	if m.admissions%staleSweepEvery == 0 {
		m.sweepStaleLocked()
	}
}

// RecordAttempt counts one credential attempt (request.intercept_after).
func (c *Collector) RecordAttempt(requestID string) {
	m := c.lifecycle
	m.mu.Lock()
	defer m.mu.Unlock()
	if req, ok := m.active[requestID]; ok {
		req.attempts++
	}
}

// ObserveCompletion records the terminal outcome of a client request.
func (c *Collector) ObserveCompletion(rec CompletionRecord) {
	m := c.lifecycle
	requested := rec.RequestedModel
	if requested == "" {
		requested = rec.Model
	}
	if requested == "" {
		// CPA also completes model-list responses (GET /v1/models) through this
		// hook. They carry no model and are not model calls, so they would only
		// skew request counts and latency.
		m.forget(rec.RequestID)
		return
	}
	format := labels.Format(rec.SourceFormat)
	model := m.modelsCap.Value(labels.Model(requested))
	stream := strconv.FormatBool(rec.Stream)
	outcome := labels.Outcome(rec.Outcome)
	code := "none"
	if rec.StatusCode > 0 {
		code = strconv.Itoa(rec.StatusCode)
	}

	m.requests.WithLabelValues(format, model, stream, outcome, code).Inc()
	if !rec.StartedAt.IsZero() && rec.CompletedAt.After(rec.StartedAt) {
		m.duration.WithLabelValues(format, model, stream, outcome).Observe(rec.CompletedAt.Sub(rec.StartedAt).Seconds())
	}

	m.mu.Lock()
	tracked, ok := m.active[rec.RequestID]
	if ok {
		delete(m.active, rec.RequestID)
		m.inFlight.WithLabelValues(tracked.format).Dec()
	}
	m.mu.Unlock()
	// Requests rejected before credential selection have zero attempts.
	if ok {
		m.attempts.WithLabelValues(format, model, outcome).Observe(float64(tracked.attempts))
	}
}

// forget stops tracking a request without recording it.
func (m *lifecycleMetrics) forget(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tracked, ok := m.active[requestID]; ok {
		delete(m.active, requestID)
		m.inFlight.WithLabelValues(tracked.format).Dec()
	}
}

// observeStreamTiming derives TTFT and generation speed from a usage record.
func (m *lifecycleMetrics) observeStreamTiming(provider, model string, rec UsageRecord) {
	if !rec.Stream || rec.Failed || rec.FailureStatusCode >= 400 || rec.TTFT <= 0 {
		return
	}
	m.ttft.WithLabelValues(provider, model).Observe(rec.TTFT.Seconds())
	generation := rec.Latency - rec.TTFT
	if rec.Detail.OutputTokens > 0 && generation > 0 {
		m.speed.WithLabelValues(provider, model).Observe(float64(rec.Detail.OutputTokens) / generation.Seconds())
	}
}

// sweepStaleLocked drops requests whose terminal event never arrived, so a lost
// request.complete cannot pin the in-flight gauge forever.
func (m *lifecycleMetrics) sweepStaleLocked() {
	cutoff := m.now().Add(-staleRequestAge)
	for id, req := range m.active {
		if req.admitted.Before(cutoff) {
			delete(m.active, id)
			m.inFlight.WithLabelValues(req.format).Dec()
		}
	}
}
