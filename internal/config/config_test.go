package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseDefaultFiveMinutes(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuotaRefreshInterval != 5*time.Minute {
		t.Fatalf("interval = %s", cfg.QuotaRefreshInterval)
	}
}

func TestParseYAMLInterval(t *testing.T) {
	yamlText := "quota-refresh-interval: 10m\ninclude-disabled: true\n"
	payload, _ := json.Marshal(map[string]any{"config_yaml": []byte(yamlText)})
	cfg, err := Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuotaRefreshInterval != 10*time.Minute {
		t.Fatalf("interval = %s", cfg.QuotaRefreshInterval)
	}
	if !cfg.IncludeDisabled {
		t.Fatal("include-disabled")
	}
}

func TestParseRejectsHugeConcurrency(t *testing.T) {
	yamlText := "max-concurrency: 1000000\n"
	payload, _ := json.Marshal(map[string]any{"config_yaml": []byte(yamlText)})
	if _, err := Parse(payload); err == nil {
		t.Fatal("expected error for huge max-concurrency")
	}
}

func TestParseRejectsOversizedYAML(t *testing.T) {
	yamlText := strings.Repeat("#", MaxConfigYAMLBytes+1)
	payload, _ := json.Marshal(map[string]any{"config_yaml": []byte(yamlText)})
	if _, err := Parse(payload); err == nil {
		t.Fatal("expected error for oversized yaml")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte(`{"config_yaml":"quota-refresh-interval: 5m\n"}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		cfg, err := Parse(raw)
		if err != nil {
			return
		}
		if cfg.QuotaRefreshInterval < time.Minute {
			t.Fatalf("interval %s", cfg.QuotaRefreshInterval)
		}
		if cfg.MaxConcurrency < 1 || cfg.MaxConcurrency > MaxConcurrency {
			t.Fatalf("concurrency %d", cfg.MaxConcurrency)
		}
	})
}
