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
	RequestedAt       time.Time
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
	quotaHasWindow   *prometheus.GaugeVec
	quotaErrors      *prometheus.CounterVec
	authSuccess      *prometheus.GaugeVec
	authFailed       *prometheus.GaugeVec
	authDisabled     *prometheus.GaugeVec
	authUnavailable  *prometheus.GaugeVec
	authNextRetry    *prometheus.GaugeVec
	authRuntimeOnly  *prometheus.GaugeVec
	authLastRefresh  *prometheus.GaugeVec
	authUpdated      *prometheus.GaugeVec
	authProjectInfo  *prometheus.GaugeVec
	lastRequest      *prometheus.GaugeVec
	modelSeen        *prometheus.GaugeVec
	modelAvailable   *prometheus.GaugeVec

	mu          sync.Mutex
	seenModels  map[string]map[string]struct{}
	emails      map[string]string
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
		emails:     map[string]string{},
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
		Help:        "Credential count by provider, status, and email (from host.auth.list).",
		ConstLabels: constLabels,
	}, []string{"provider", "status", "email", "account_type"})
	c.modelsSeen = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_models_seen",
		Help:        "Unique models observed via usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider"})
	c.requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_requests_total",
		Help:        "Completed proxy requests observed by usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider", "model", "auth_index", "email"})
	c.failures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_failures_total",
		Help:        "Failed proxy requests observed by usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider", "model", "auth_index", "email", "code"})
	c.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "cliproxy_request_duration_seconds",
		Help:        "Proxy request latency from usage.handle.",
		ConstLabels: constLabels,
		Buckets:     prometheus.DefBuckets,
	}, []string{"provider", "model", "auth_index", "email"})
	c.tokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_tokens_total",
		Help:        "Token counts from usage.handle Detail fields.",
		ConstLabels: constLabels,
	}, []string{"provider", "model", "auth_index", "email", "type"})
	c.quotaUsed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_used_ratio",
		Help:        "Provider quota used ratio (0-1) per account window.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email", "window"})
	c.quotaRemaining = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_remaining_ratio",
		Help:        "Provider quota remaining ratio (0-1) per account window.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email", "window"})
	c.quotaReset = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_reset_timestamp_seconds",
		Help:        "Unix timestamp when a quota window resets.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email", "window"})
	c.quotaLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_last_success_timestamp_seconds",
		Help:        "Unix timestamp of the last successful quota fetch for an account.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.quotaSupported = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_supported",
		Help:        "1 if the plugin knows how to fetch quota for this credential.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.quotaErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cliproxy_quota_fetch_errors_total",
		Help:        "Quota fetch errors isolated per provider.",
		ConstLabels: constLabels,
	}, []string{"provider", "reason"})
	c.quotaHasWindow = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_quota_has_window",
		Help:        "1 if this credential currently exposes a quota window; 0 for pay-as-you-go or empty quota payloads.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_success",
		Help:        "Recent successful request count from host.auth.list (not a Prom counter; host snapshot).",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authFailed = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_failed",
		Help:        "Recent failed request count from host.auth.list (not a Prom counter; host snapshot).",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authDisabled = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_disabled",
		Help:        "1 if host.auth.list reports the credential as disabled.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authUnavailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_unavailable",
		Help:        "1 if host.auth.list reports the credential as unavailable.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authNextRetry = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_next_retry_timestamp_seconds",
		Help:        "Unix timestamp of host.auth.list next_retry_after when cooling down.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authRuntimeOnly = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_runtime_only",
		Help:        "1 if host.auth.list reports the credential as runtime-only (no backing auth file).",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authLastRefresh = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_last_refresh_timestamp_seconds",
		Help:        "Unix timestamp of host.auth.list last_refresh when present.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authUpdated = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_updated_timestamp_seconds",
		Help:        "Unix timestamp of host.auth.list updated_at when present.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email"})
	c.authProjectInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_auth_project_info",
		Help:        "1 if host.auth.list reports a project_id for this credential.",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email", "project_id"})
	c.lastRequest = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_last_request_timestamp_seconds",
		Help:        "Unix timestamp of the last usage.handle record for this provider, model, and credential.",
		ConstLabels: constLabels,
	}, []string{"provider", "model", "auth_index", "email"})
	c.modelSeen = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_model_seen",
		Help:        "1 if this model has been observed via usage.handle.",
		ConstLabels: constLabels,
	}, []string{"provider", "model"})
	c.modelAvailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cliproxy_model_available",
		Help:        "1 if host.auth.get_runtime reports this model for the credential (0 if unavailable).",
		ConstLabels: constLabels,
	}, []string{"provider", "auth_index", "email", "model", "status"})

	reg.MustRegister(
		c.info, c.up, c.pollInterval, c.credentials, c.modelsSeen, c.modelSeen, c.modelAvailable,
		c.requests, c.failures, c.duration, c.tokens,
		c.quotaUsed, c.quotaRemaining, c.quotaReset, c.quotaLastSuccess, c.quotaSupported, c.quotaHasWindow, c.quotaErrors,
		c.authSuccess, c.authFailed, c.authDisabled, c.authUnavailable, c.authNextRetry,
		c.authRuntimeOnly, c.authLastRefresh, c.authUpdated, c.authProjectInfo, c.lastRequest,
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
	authIndex := labels.AuthIndex(rec.AuthIndex)
	email := c.emailFor(authIndex)
	c.requests.WithLabelValues(provider, model, authIndex, email).Inc()
	if rec.Latency > 0 {
		c.duration.WithLabelValues(provider, model, authIndex, email).Observe(rec.Latency.Seconds())
	}
	failed := rec.Failed || rec.FailureStatusCode >= 400
	if failed {
		code := "unknown"
		if rec.FailureStatusCode > 0 {
			code = strconv.Itoa(rec.FailureStatusCode)
		}
		c.failures.WithLabelValues(provider, model, authIndex, email, code).Inc()
	}
	add := func(kind string, n int64) {
		if n == 0 {
			return
		}
		c.tokens.WithLabelValues(provider, model, authIndex, email, kind).Add(float64(n))
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
	c.modelSeen.WithLabelValues(provider, model).Set(1)
	requested := rec.RequestedAt
	if requested.IsZero() {
		requested = time.Now().UTC()
	}
	c.lastRequest.WithLabelValues(provider, model, authIndex, email).Set(float64(requested.Unix()))
}

func (c *Collector) ApplyCredentials(creds []quota.Credential) {
	c.credentials.Reset()
	c.authSuccess.Reset()
	c.authFailed.Reset()
	c.authDisabled.Reset()
	c.authUnavailable.Reset()
	c.authNextRetry.Reset()
	c.authRuntimeOnly.Reset()
	c.authLastRefresh.Reset()
	c.authUpdated.Reset()
	c.authProjectInfo.Reset()
	c.modelAvailable.Reset()
	c.mu.Lock()
	c.emails = map[string]string{}
	c.mu.Unlock()
	counts := map[[4]string]int{}
	var models []quota.ModelAvailability
	for _, cred := range creds {
		provider := labels.Provider(cred.Provider)
		status := labels.Status(cred.Status)
		authIndex := labels.AuthIndex(cred.AuthIndex)
		email := labels.Email(cred.Email)
		accountType := labels.AccountType(cred.AccountType)
		c.mu.Lock()
		c.emails[authIndex] = email
		c.mu.Unlock()
		counts[[4]string{provider, status, email, accountType}]++
		c.authSuccess.WithLabelValues(provider, authIndex, email).Set(float64(cred.Success))
		c.authFailed.WithLabelValues(provider, authIndex, email).Set(float64(cred.Failed))
		disabled := 0.0
		if cred.Disabled {
			disabled = 1
		}
		unavailable := 0.0
		if cred.Unavailable {
			unavailable = 1
		}
		c.authDisabled.WithLabelValues(provider, authIndex, email).Set(disabled)
		c.authUnavailable.WithLabelValues(provider, authIndex, email).Set(unavailable)
		if cred.NextRetryUnix > 0 {
			c.authNextRetry.WithLabelValues(provider, authIndex, email).Set(float64(cred.NextRetryUnix))
		}
		runtimeOnly := 0.0
		if cred.RuntimeOnly {
			runtimeOnly = 1
		}
		c.authRuntimeOnly.WithLabelValues(provider, authIndex, email).Set(runtimeOnly)
		if cred.LastRefreshUnix > 0 {
			c.authLastRefresh.WithLabelValues(provider, authIndex, email).Set(float64(cred.LastRefreshUnix))
		}
		if cred.UpdatedAtUnix > 0 {
			c.authUpdated.WithLabelValues(provider, authIndex, email).Set(float64(cred.UpdatedAtUnix))
		}
		if projectID := labels.ProjectID(cred.ProjectID); projectID != "unknown" && strings.TrimSpace(cred.ProjectID) != "" {
			c.authProjectInfo.WithLabelValues(provider, authIndex, email, projectID).Set(1)
		}
		models = append(models, cred.Models...)
	}
	for key, n := range counts {
		c.credentials.WithLabelValues(key[0], key[1], key[2], key[3]).Set(float64(n))
	}
	if len(models) > 0 {
		c.ApplyModelAvailability(models)
	}
}

func (c *Collector) emailFor(authIndex string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if email, ok := c.emails[authIndex]; ok && email != "" {
		return email
	}
	return "unknown"
}

func (c *Collector) ApplyModelAvailability(models []quota.ModelAvailability) {
	c.modelAvailable.Reset()
	for _, m := range models {
		provider := labels.Provider(m.Provider)
		authIndex := labels.AuthIndex(m.AuthIndex)
		email := labels.Email(m.Email)
		model := labels.Model(m.Model)
		status := labels.Status(m.Status)
		value := 1.0
		if m.Unavailable {
			value = 0
			if status == "unknown" || status == "" {
				status = "unavailable"
			}
		}
		c.modelAvailable.WithLabelValues(provider, authIndex, email, model, status).Set(value)
	}
}

func (c *Collector) ApplyQuota(accounts []quota.Account) {
	c.quotaUsed.Reset()
	c.quotaRemaining.Reset()
	c.quotaReset.Reset()
	c.quotaLastSuccess.Reset()
	c.quotaSupported.Reset()
	c.quotaHasWindow.Reset()
	for _, account := range accounts {
		provider := labels.Provider(account.Provider)
		authIndex := labels.AuthIndex(account.AuthIndex)
		email := labels.Email(account.Email)
		supported := 0.0
		if account.Supported {
			supported = 1
		}
		c.quotaSupported.WithLabelValues(provider, authIndex, email).Set(supported)
		hasWindow := 0.0
		if len(account.Windows) > 0 {
			hasWindow = 1
		}
		c.quotaHasWindow.WithLabelValues(provider, authIndex, email).Set(hasWindow)
		if account.Error != "" {
			c.quotaErrors.WithLabelValues(provider, reasonLabel(account.Error)).Inc()
		}
		if !account.FetchedAt.IsZero() && account.Error == "" {
			c.quotaLastSuccess.WithLabelValues(provider, authIndex, email).Set(float64(account.FetchedAt.Unix()))
		}
		for _, window := range account.Windows {
			wid := labels.Window(window.ID)
			c.quotaUsed.WithLabelValues(provider, authIndex, email, wid).Set(clampRatio(window.UsedRatio))
			c.quotaRemaining.WithLabelValues(provider, authIndex, email, wid).Set(clampRatio(window.RemainingRatio))
			if window.ResetUnix > 0 {
				c.quotaReset.WithLabelValues(provider, authIndex, email, wid).Set(float64(window.ResetUnix))
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
