package plugin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

// slowHost blocks inside ListAuth to simulate a quota poll stuck on a slow
// upstream, which is exactly when a reconfigure is most likely to collide.
type slowHost struct{ release chan struct{} }

func (h *slowHost) ListAuth() ([]quota.AuthFile, error) {
	<-h.release
	return nil, nil
}
func (h *slowHost) GetAuthJSON(string) ([]byte, error)           { return nil, nil }
func (h *slowHost) GetRuntime(string) (quota.RuntimeAuth, error) { return quota.RuntimeAuth{}, nil }
func (h *slowHost) DoHTTP(quota.HTTPRequest) (quota.HTTPResponse, error) {
	return quota.HTTPResponse{}, nil
}

func TestReconfigureDoesNotBlockUsageHandling(t *testing.T) {
	h := &slowHost{release: make(chan struct{})}
	rt := NewRuntime(h)
	_ = rt.Handle("plugin.register", nil)
	time.Sleep(100 * time.Millisecond) // let the first poll enter ListAuth

	reconfigured := make(chan struct{})
	go func() {
		payload, _ := json.Marshal(map[string]any{"config_yaml": []byte("quota-refresh-interval: 10m\n")})
		_ = rt.Handle("plugin.reconfigure", payload)
		close(reconfigured)
	}()
	time.Sleep(100 * time.Millisecond) // let reconfigure reach the poller swap

	done := make(chan struct{})
	go func() {
		_ = rt.Handle("usage.handle", []byte(`{"Provider":"xai","Model":"m","Detail":{"TotalTokens":1}}`))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(h.release)
		t.Fatal("usage.handle blocked behind an in-flight quota poll during reconfigure")
	}
	close(h.release)
	<-reconfigured
}
