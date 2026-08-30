package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
	"gopkg.in/yaml.v3"
)

const (
	MaxConfigYAMLBytes = 64 << 10
	MaxConcurrency     = 16
	maxQuotaInterval   = 24 * time.Hour
	maxRequestTimeout  = 2 * time.Minute
)

type Config struct {
	QuotaRefreshInterval time.Duration
	RequestTimeout       time.Duration
	IncludeDisabled      bool
	ScrapeToken          string
	MaxConcurrency       int
}

func Default() Config {
	d := quota.DefaultConfig()
	return Config{
		QuotaRefreshInterval: d.Interval,
		RequestTimeout:       d.RequestTimeout,
		MaxConcurrency:       d.MaxConcurrency,
	}
}

type lifecycleRequest struct {
	ConfigYAML json.RawMessage `json:"config_yaml"`
}

func Parse(request []byte) (Config, error) {
	cfg := Default()
	if len(request) == 0 {
		return cfg, nil
	}
	var req lifecycleRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return cfg, fmt.Errorf("decode lifecycle: %w", err)
	}
	text, err := configYAMLText(req.ConfigYAML)
	if err != nil {
		return cfg, err
	}
	if strings.TrimSpace(text) == "" {
		return cfg, nil
	}
	if len(text) > MaxConfigYAMLBytes {
		return cfg, fmt.Errorf("plugin config yaml exceeds %d bytes", MaxConfigYAMLBytes)
	}
	var raw struct {
		QuotaRefreshInterval string `yaml:"quota-refresh-interval"`
		RequestTimeout       string `yaml:"request-timeout"`
		IncludeDisabled      *bool  `yaml:"include-disabled"`
		ScrapeToken          string `yaml:"scrape-token"`
		MaxConcurrency       int    `yaml:"max-concurrency"`
	}
	if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
		return cfg, fmt.Errorf("decode plugin config yaml: %w", err)
	}
	if raw.QuotaRefreshInterval != "" {
		d, err := time.ParseDuration(raw.QuotaRefreshInterval)
		if err != nil || d < time.Minute || d > maxQuotaInterval {
			return cfg, fmt.Errorf("quota-refresh-interval must be a Go duration between 1m and 24h")
		}
		cfg.QuotaRefreshInterval = d
	}
	if raw.RequestTimeout != "" {
		d, err := time.ParseDuration(raw.RequestTimeout)
		if err != nil || d < time.Second || d > maxRequestTimeout {
			return cfg, fmt.Errorf("request-timeout must be a Go duration between 1s and 2m")
		}
		cfg.RequestTimeout = d
	}
	if raw.IncludeDisabled != nil {
		cfg.IncludeDisabled = *raw.IncludeDisabled
	}
	cfg.ScrapeToken = strings.TrimSpace(raw.ScrapeToken)
	if raw.MaxConcurrency > MaxConcurrency {
		return cfg, fmt.Errorf("max-concurrency must be <= %d", MaxConcurrency)
	}
	if raw.MaxConcurrency > 0 {
		cfg.MaxConcurrency = raw.MaxConcurrency
	}
	return cfg, nil
}

func configYAMLText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var asBytes []byte
	if err := json.Unmarshal(raw, &asBytes); err == nil {
		return string(asBytes), nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	return string(raw), nil
}

func (c Config) Quota() quota.Config {
	return quota.Config{
		Interval:        c.QuotaRefreshInterval,
		RequestTimeout:  c.RequestTimeout,
		IncludeDisabled: c.IncludeDisabled,
		MaxConcurrency:  c.MaxConcurrency,
	}
}
