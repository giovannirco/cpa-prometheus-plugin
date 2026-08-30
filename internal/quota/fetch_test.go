package quota

import (
	"fmt"
	"strings"
	"testing"
)

type fakeHost struct {
	files []AuthFile
	json  map[string][]byte
	http  func(HTTPRequest) (HTTPResponse, error)
}

func (f *fakeHost) ListAuth() ([]AuthFile, error) { return f.files, nil }
func (f *fakeHost) GetRuntime(string) (RuntimeAuth, error) {
	return RuntimeAuth{}, nil
}
func (f *fakeHost) GetAuthJSON(authIndex string) ([]byte, error) {
	raw, ok := f.json[authIndex]
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	return raw, nil
}
func (f *fakeHost) DoHTTP(req HTTPRequest) (HTTPResponse, error) {
	if f.http != nil {
		return f.http(req)
	}
	return HTTPResponse{StatusCode: 200, Body: []byte(`{"five_hour":{"utilization":0.1}}`)}, nil
}

func TestPollFetchesClaudeAndIsolates429(t *testing.T) {
	host := &fakeHost{
		files: []AuthFile{
			{AuthIndex: "ok", Provider: "claude", Status: "active"},
			{AuthIndex: "rl", Provider: "claude", Status: "active"},
			{AuthIndex: "cursor", Provider: "cursor", Status: "active"},
		},
		json: map[string][]byte{
			"ok":     []byte(`{"access_token":"tok-ok"}`),
			"rl":     []byte(`{"access_token":"tok-rl"}`),
			"cursor": []byte(`{"access_token":"tok-c"}`),
		},
		http: func(req HTTPRequest) (HTTPResponse, error) {
			if strings.Contains(strings.Join(req.Headers["Authorization"], ""), "tok-rl") {
				return HTTPResponse{StatusCode: 429, Body: []byte(`rate`)}, nil
			}
			return HTTPResponse{StatusCode: 200, Body: []byte(`{"five_hour":{"utilization":0.3,"resets_at":"2026-08-30T01:00:00Z"}}`)}, nil
		},
	}
	accounts, creds, err := Poll(host, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 3 {
		t.Fatalf("creds=%d", len(creds))
	}
	var okAcc, rlAcc, cursorAcc *Account
	for i := range accounts {
		switch accounts[i].AuthIndex {
		case "ok":
			okAcc = &accounts[i]
		case "rl":
			rlAcc = &accounts[i]
		case "cursor":
			cursorAcc = &accounts[i]
		}
	}
	if okAcc == nil || !okAcc.Supported || len(okAcc.Windows) != 1 || okAcc.Windows[0].UsedRatio != 0.3 {
		t.Fatalf("ok account %#v", okAcc)
	}
	if rlAcc == nil || rlAcc.Error == "" || !strings.Contains(rlAcc.Error, "429") {
		t.Fatalf("rate-limited account %#v", rlAcc)
	}
	if cursorAcc == nil || cursorAcc.Supported || cursorAcc.Error != "" || len(cursorAcc.Windows) != 0 {
		t.Fatalf("unsupported provider should be listed without fetch: %#v", cursorAcc)
	}
	for _, acc := range accounts {
		if strings.Contains(acc.Error, "tok-") || strings.Contains(fmt.Sprintf("%#v", acc), "tok-") {
			t.Fatalf("token leaked into account: %#v", acc)
		}
	}
}

func TestPollCopiesAuthNumericsOntoCredentials(t *testing.T) {
	host := &fakeHost{
		files: []AuthFile{{
			AuthIndex: "a1", Provider: "xai", Status: "active",
			Unavailable: true, Success: 12, Failed: 3, NextRetryUnix: 1_800_000_000,
		}},
		json: map[string][]byte{"a1": []byte(`{"access_token":"x"}`)},
		http: func(HTTPRequest) (HTTPResponse, error) {
			return HTTPResponse{StatusCode: 200, Body: []byte(`{"config":{}}`)}, nil
		},
	}
	accounts, creds, err := Poll(host, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Success != 12 || creds[0].Failed != 3 || !creds[0].Unavailable || creds[0].NextRetryUnix != 1_800_000_000 {
		t.Fatalf("creds %#v", creds)
	}
	if len(accounts) != 1 || !accounts[0].Supported || len(accounts[0].Windows) != 0 {
		t.Fatalf("PAYG account should be supported with empty windows: %#v", accounts)
	}
}

func TestPollSkipsDisabledUnlessConfigured(t *testing.T) {
	host := &fakeHost{
		files: []AuthFile{{AuthIndex: "d1", Provider: "xai", Disabled: true, Status: "disabled"}},
		json:  map[string][]byte{"d1": []byte(`{"access_token":"x"}`)},
	}
	accounts, creds, err := Poll(host, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Status != "disabled" {
		t.Fatalf("creds %#v", creds)
	}
	if len(accounts) != 0 {
		t.Fatalf("disabled should be skipped: %#v", accounts)
	}
	cfg := DefaultConfig()
	cfg.IncludeDisabled = true
	accounts, _, err = Poll(host, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("include-disabled: %#v", accounts)
	}
}
