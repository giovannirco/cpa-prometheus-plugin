package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/collector"
	"github.com/giovannirco/cpa-prometheus-plugin/internal/config"
	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

const (
	PluginID      = "cpa-prometheus"
	PluginName    = "CPA Prometheus"
	PluginAuthor  = "giovannirco"
	PluginRepo    = "https://github.com/giovannirco/cpa-prometheus-plugin"
	schemaVersion = 1

	metricsResourcePath = "/metrics"
	metricsManagePath   = "/plugins/cpa-prometheus/metrics"
)

var PluginVersion = "0.1.7"

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Runtime struct {
	mu     sync.Mutex
	col    *collector.Collector
	cfg    config.Config
	host   quota.Host
	poller *quota.Poller
}

func NewRuntime(host quota.Host) *Runtime {
	col := collector.New(PluginVersion)
	return &Runtime{col: col, cfg: config.Default(), host: host}
}

func (rt *Runtime) Handle(method string, request []byte) []byte {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return rt.register(request)
	case "plugin.shutdown":
		rt.shutdown()
		return okJSON(map[string]any{})
	case "usage.handle":
		return rt.handleUsage(request)
	case "management.register":
		return okJSON(map[string]any{
			"routes": []map[string]string{{
				"Method":      http.MethodGet,
				"Path":        metricsManagePath,
				"Description": "Prometheus text exposition of CPA usage, dashboard counts, and quota gauges.",
			}},
			"resources": []map[string]string{{
				"Path":        metricsResourcePath,
				"Menu":        "Prometheus",
				"Description": "Prometheus metrics for Alloy scrape. Path /v0/resource/plugins/cpa-prometheus/metrics.",
			}},
		})
	case "management.handle":
		return rt.handleManagement(request)
	default:
		return errorJSON("unknown_method", "unknown method: "+method)
	}
}

func (rt *Runtime) register(request []byte) []byte {
	cfg, err := config.Parse(request)
	if err != nil {
		return errorJSON("invalid_config", err.Error())
	}
	rt.mu.Lock()
	rt.cfg = cfg
	if rt.col == nil {
		rt.col = collector.New(PluginVersion)
	}
	rt.col.SetScrapeToken(cfg.ScrapeToken)
	rt.col.SetPollInterval(cfg.QuotaRefreshInterval)
	host := rt.host
	rt.mu.Unlock()
	rt.startPoller(host, cfg)
	return okJSON(map[string]any{
		"schema_version": schemaVersion,
		"metadata": map[string]any{
			"Name":             PluginName,
			"Version":          PluginVersion,
			"Author":           PluginAuthor,
			"GitHubRepository": PluginRepo,
			"Logo":             "",
			"ConfigFields": []map[string]string{
				{"Name": "quota-refresh-interval", "Type": "string", "Description": "Quota poll interval. Default 5m."},
				{"Name": "request-timeout", "Type": "string", "Description": "Per-account quota HTTP timeout. Default 20s."},
				{"Name": "include-disabled", "Type": "boolean", "Description": "Include disabled credentials in quota scans."},
				{"Name": "scrape-token", "Type": "string", "Description": "Optional bearer token required on /metrics."},
			},
		},
		"capabilities": map[string]bool{
			"usage_plugin":   true,
			"management_api": true,
		},
	})
}

func (rt *Runtime) startPoller(host quota.Host, cfg config.Config) {
	if host == nil {
		return
	}
	rt.mu.Lock()
	if rt.poller != nil {
		rt.poller.Stop()
	}
	p := quota.NewPoller(cfg.QuotaRefreshInterval)
	rt.poller = p
	col := rt.col
	rt.mu.Unlock()
	p.Start(func() {
		accounts, creds, err := quota.Poll(host, cfg.Quota())
		if err != nil {
			return
		}
		col.ApplyCredentials(creds)
		col.ApplyQuota(accounts)
	})
}

func (rt *Runtime) shutdown() {
	rt.mu.Lock()
	p := rt.poller
	rt.poller = nil
	rt.mu.Unlock()
	if p != nil {
		p.Stop()
	}
}

type usageJSON struct {
	Provider    string       `json:"Provider"`
	Model       string       `json:"Model"`
	AuthIndex   string       `json:"AuthIndex"`
	Latency     int64        `json:"Latency"`
	Failed      bool         `json:"Failed"`
	Failure     usageFailure `json:"Failure"`
	Detail      usageDetail  `json:"Detail"`
	APIKey      string       `json:"APIKey"`
	RequestedAt time.Time    `json:"RequestedAt"`
}

type usageFailure struct {
	StatusCode int    `json:"StatusCode"`
	Body       string `json:"Body"`
}

type usageDetail struct {
	InputTokens         int64 `json:"InputTokens"`
	OutputTokens        int64 `json:"OutputTokens"`
	ReasoningTokens     int64 `json:"ReasoningTokens"`
	CachedTokens        int64 `json:"CachedTokens"`
	CacheReadTokens     int64 `json:"CacheReadTokens"`
	CacheCreationTokens int64 `json:"CacheCreationTokens"`
	TotalTokens         int64 `json:"TotalTokens"`
}

func (rt *Runtime) handleUsage(request []byte) []byte {
	if len(request) == 0 {
		return okJSON(map[string]any{"ignored": true})
	}
	var rec usageJSON
	if err := json.Unmarshal(request, &rec); err != nil {
		return okJSON(map[string]any{"ignored": true})
	}
	_ = rec.APIKey
	_ = rec.Failure.Body
	rt.mu.Lock()
	col := rt.col
	rt.mu.Unlock()
	if col == nil {
		return okJSON(map[string]any{"ignored": true})
	}
	col.ObserveUsage(collector.UsageRecord{
		Provider:          rec.Provider,
		Model:             rec.Model,
		AuthIndex:         rec.AuthIndex,
		RequestedAt:       rec.RequestedAt,
		Latency:           time.Duration(rec.Latency),
		Failed:            rec.Failed,
		FailureStatusCode: rec.Failure.StatusCode,
		Detail: collector.TokenDetail{
			InputTokens:         rec.Detail.InputTokens,
			OutputTokens:        rec.Detail.OutputTokens,
			ReasoningTokens:     rec.Detail.ReasoningTokens,
			CachedTokens:        rec.Detail.CachedTokens,
			CacheReadTokens:     rec.Detail.CacheReadTokens,
			CacheCreationTokens: rec.Detail.CacheCreationTokens,
			TotalTokens:         rec.Detail.TotalTokens,
		},
	})
	return okJSON(map[string]any{"stored": true})
}

type managementRequest struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers"`
}

type managementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers"`
	Body       []byte              `json:"Body"`
}

func (rt *Runtime) handleManagement(request []byte) []byte {
	var req managementRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &req); err != nil {
			return errorJSON("invalid_request", err.Error())
		}
	}
	if req.Method != "" && !strings.EqualFold(req.Method, http.MethodGet) {
		return okJSON(managementResponse{StatusCode: http.StatusMethodNotAllowed, Headers: map[string][]string{"content-type": {"text/plain"}}, Body: []byte("method not allowed")})
	}
	if req.Path != "" && !isMetricsPath(req.Path) {
		return okJSON(managementResponse{StatusCode: http.StatusNotFound, Headers: map[string][]string{"content-type": {"text/plain"}}, Body: []byte("not found")})
	}
	rt.mu.Lock()
	col := rt.col
	rt.mu.Unlock()
	if col == nil {
		return okJSON(managementResponse{StatusCode: 500, Body: []byte("collector unavailable")})
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodGet, metricsResourcePath, nil)
	for k, vs := range req.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	col.MetricsHandler().ServeHTTP(rec, httpReq)
	headers := map[string][]string{}
	for k, vs := range rec.Header() {
		headers[k] = vs
	}
	return okJSON(managementResponse{StatusCode: rec.Code, Headers: headers, Body: rec.Body.Bytes()})
}

func isMetricsPath(path string) bool {
	path = strings.TrimRight(path, "/")
	return strings.HasSuffix(path, metricsResourcePath) || strings.HasSuffix(path, metricsManagePath)
}

func okJSON(v any) []byte {
	result, err := json.Marshal(v)
	if err != nil {
		return errorJSON("plugin_error", err.Error())
	}
	raw, _ := json.Marshal(envelope{OK: true, Result: result})
	return raw
}

func errorJSON(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func (rt *Runtime) Collector() *collector.Collector {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.col
}
