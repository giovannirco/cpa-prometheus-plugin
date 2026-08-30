package config

import (
	"encoding/json"
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
