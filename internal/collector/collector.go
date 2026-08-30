package collector

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/labels"
	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

const (
	DefaultQuotaRefreshInterval = 5 * time.Minute
	pluginID                    = "cpa-prometheus"
)

type TokenDetail struct {
	InputTokens         int64
	OutputTokens        int64
	ReasoningTokens     int64
	CachedTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	TotalTokens         int64
}

type UsageRecord struct {
	Provider          string
	Model             string
	AuthIndex         string
	Latency           time.Duration
	Failed            bool
	FailureStatusCode int
	Detail            TokenDetail
}

type Collector struct {
	reg     *prometheus.Registry
	version string

	info             *prometheus.GaugeVec
	up               prometheus.Gauge
	pollInterval     prometheus.Gauge
	credentials      *prometheus.GaugeVec
	modelsSeen       *prometheus.GaugeVec
	requests         *prometheus.CounterVec
	failures         *prometheus.CounterVec
	duration         *prometheus.HistogramVec
	tokens           *prometheus.CounterVec
	quotaUsed        *prometheus.GaugeVec
	quotaRemaining   *prometheus.GaugeVec
	quotaReset       *prometheus.GaugeVec
	quotaLastSuccess *prometheus.GaugeVec
	quotaSupported   *prometheus.GaugeVec
	quotaErrors      *prometheus.CounterVec

	mu          sync.Mutex
	seenModels  map[string]map[string]struct{}
	scrapeToken string
}

func New(version string) *Collector {
	if strings.TrimSpace(version) == "" {
		version = "0.0.0"
	}
	reg := prometheus.NewRegistry()
	c := &Collector{
		reg:        reg,
		version:    version,
		seenModels: map[string]map[string]struct{}{},
	}
	constLabels := prometheus.Labels{"plugin_id": pluginID}
	c.info = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_info",
		Help:        "CLIProxyAPI prometheus plugin metadata.",
		ConstLabels: constLabels,
	}, []string{"plugin_version"})
	c.up = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "cliproxy_up",
		Help:        "1 if the CPA prometheus plugin is loaded.",
		ConstLabels: constLabels,
	})
	c.pollInterval = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_poll_interval_seconds",
		Help:        "Configured quota refresh interval in seconds.",
		ConstLabels: constLabels,
	})
	c.credentials = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_credentials",
		Help:        "Credential count by provider and runtime status (from host.auth.list).",
		ConstLabels: constLabels,
	}, []string{"provider", "status"})
	c.modelsSeen = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_models_seen",
		Help:        "Unique models observed via usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider"})
	c.requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_requests_total",
		Help:        "Completed proxy requests observed by usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider", "model"})
	c.failures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_failures_total",
		Help:        "Failed proxy requests observed by usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider", "model", "code"})
	c.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "cliproxy_request_duration_seconds",
		Help:        "Proxy request latency from usage.handle.",
		ConstLabels: constLabels,
		Buckets:     prometheus.DefBuckets,
	}, []string{"provider", "model"})
	c.tokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_tokens_total",
		Help:        "Token counts from usage.handle Detail fields.",
		ConstLabels: constLabels,
	}, []string{"provider", "model", "type"})
	c.quotaUsed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_used_ratio",
		Help:        "Provider quota used ratio (0-1) per account window.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "window"})
	c.quotaRemaining = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_remaining_ratio",
		Help:        "Provider quota remaining ratio (0-1) per account window.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "window"})
	c.quotaReset = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_reset_timestamp_seconds",
		Help:        "Unix timestamp when a quota window resets.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "window"})
	c.quotaLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_last_success_timestamp_seconds",
		Help:        "Unix timestamp of the last successful quota fetch for an account.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index"})
	c.quotaSupported = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_supported",
		Help:        "1 if the plugin knows how to fetch quota for this credential.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index"})
	c.quotaErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_quota_fetch_errors_total",
		Help:        "Quota fetch errors isolated per provider.",
		ConstLabels: constLabels,
	}, []string{"provider", "reason"})

	reg.MustRegister(
		c.info, c.up, c.pollInterval, c.credentials, c.modelsSeen,
		c.requests, c.failures, c.duration, c.tokens,
		c.quotaUsed, c.quotaRemaining, c.quotaReset, c.quotaLastSuccess, c.quotaSupported, c.quotaErrors,
	)
	c.info.WithLabelValues(version).Set(1)
	c.up.Set(1)
	c.pollInterval.Set(DefaultQuotaRefreshInterval.Seconds())
	return c
}

func (c *Collector) SetScrapeToken(token string) {
	c.mu.Lock()
	c.scrapeToken = strings.TrimSpace(token)
	c.mu.Unlock()
}

func (c *Collector) SetPollInterval(d time.Duration) {
	if d <= 0 {
		d = DefaultQuotaRefreshInterval
	}
	c.pollInterval.Set(d.Seconds())
}

func (c *Collector) ObserveUsage(rec UsageRecord) {
	provider := labels.Provider(rec.Provider)
	model := labels.Model(rec.Model)
	c.requests.WithLabelValues(provider, model).Inc()
	if rec.Latency > 0 {
		c.duration.WithLabelValues(provider, model).Observe(rec.Latency.Seconds())
	}
	failed := rec.Failed || rec.FailureStatusCode >= 400
	if failed {
		code := "unknown"
		if rec.FailureStatusCode > 0 {
			code = strconv.Itoa(rec.FailureStatusCode)
		}
		c.failures.WithLabelValues(provider, model, code).Inc()
	}
	add := func(kind string, n int64) {
		if n == 0 {
			return
		}
		c.tokens.WithLabelValues(provider, model, kind).Add(float64(n))
	}
	add("input", rec.Detail.InputTokens)
	add("output", rec.Detail.OutputTokens)
	add("reasoning", rec.Detail.ReasoningTokens)
	add("cached", rec.Detail.CachedTokens)
	add("cache_read", rec.Detail.CacheReadTokens)
	add("cache_creation", rec.Detail.CacheCreationTokens)
	add("total", rec.Detail.TotalTokens)

	c.mu.Lock()
	if c.seenModels[provider] == nil {
		c.seenModels[provider] = map[string]struct{}{}
	}
	c.seenModels[provider][model] = struct{}{}
	n := len(c.seenModels[provider])
	c.mu.Unlock()
	c.modelsSeen.WithLabelValues(provider).Set(float64(n))
}

func (c *Collector) ApplyCredentials(creds []quota.Credential) {
	c.credentials.Reset()
	counts := map[[2]string]int{}
	for _, cred := range creds {
		key := [2]string{labels.Provider(cred.Provider), labels.Status(cred.Status)}
		counts[key]++
	}
	for key, n := range counts {
		c.credentials.WithLabelValues(key[0], key[1]).Set(float64(n))
	}
}

func (c *Collector) ApplyQuota(accounts []quota.Account) {
	c.quotaUsed.Reset()
	c.quotaRemaining.Reset()
	c.quotaReset.Reset()
	c.quotaLastSuccess.Reset()
	c.quotaSupported.Reset()
	for _, account := range accounts {
		provider := labels.Provider(account.Provider)
		authIndex := labels.AuthIndex(account.AuthIndex)
		supported := 0.0
		if account.Supported {
			supported = 1
		}
		c.quotaSupported.WithLabelValues(provider, authIndex).Set(supported)
		if account.Error != "" {
			c.quotaErrors.WithLabelValues(provider, reasonLabel(account.Error)).Inc()
		}
		if !account.FetchedAt.IsZero() && account.Error == "" {
			c.quotaLastSuccess.WithLabelValues(provider, authIndex).Set(float64(account.FetchedAt.Unix()))
		}
		for _, window := range account.Windows {
			wid := labels.Window(window.ID)
			c.quotaUsed.WithLabelValues(provider, authIndex, wid).Set(clampRatio(window.UsedRatio))
			c.quotaRemaining.WithLabelValues(provider, authIndex, wid).Set(clampRatio(window.RemainingRatio))
			if window.ResetUnix > 0 {
				c.quotaReset.WithLabelValues(provider, authIndex, wid).Set(float64(window.ResetUnix))
			}
		}
	}
}

func reasonLabel(err string) string {
	err = strings.ToLower(err)
	switch {
	case strings.Contains(err, "429") || strings.Contains(err, "rate"):
		return "rate_limited"
	case strings.Contains(err, "401") || strings.Contains(err, "403"):
		return "unauthorized"
	case strings.Contains(err, "timeout"):
		return "timeout"
	case strings.Contains(err, "incomplete"):
		return "credential_incomplete"
	default:
		return "fetch_failed"
	}
}

func clampRatio(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (c *Collector) Gather() (string, error) {
	families, err := c.reg.Gather()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	for _, family := range families {
		if _, err := expfmt.MetricFamilyToText(&buf, family); err != nil {
			return "", fmt.Errorf("encode %s: %w", family.GetName(), err)
		}
	}
	return buf.String(), nil
}

func (c *Collector) MetricsHandler() http.Handler {
	inner := promhttp.HandlerFor(c.reg, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		token := c.scrapeToken
		c.mu.Unlock()
		if token != "" && !scrapeTokenOK(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

func scrapeTokenOK(r *http.Request, token string) bool {
	if r == nil {
		return false
	}
	if got := strings.TrimSpace(r.Header.Get("X-Scrape-Token")); got != "" && got == token {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return strings.EqualFold(strings.TrimPrefix(auth, "Bearer "), token) || strings.TrimPrefix(auth, "Bearer ") == token
}

func WriteFamilies(w io.Writer, families []*dto.MetricFamily) error {
	for _, family := range families {
		if _, err := expfmt.MetricFamilyToText(w, family); err != nil {
			return err
		}
	}
	return nil
}
