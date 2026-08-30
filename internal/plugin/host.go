package plugin

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/quota"
)

type CallbackFunc func(method string, request []byte) ([]byte, error)

type callbackHost struct {
	call CallbackFunc
}

func NewCallbackHost(call CallbackFunc) quota.Host {
	return callbackHost{call: call}
}

type hostAuthListResponse struct {
	Files []hostAuthFile `json:"files"`
}

type hostAuthFile struct {
	AuthIndex      string                    `json:"auth_index"`
	Provider       string                    `json:"provider"`
	Type           string                    `json:"type"`
	Status         string                    `json:"status"`
	Disabled       bool                      `json:"disabled"`
	Unavailable    bool                      `json:"unavailable"`
	RuntimeOnly    bool                      `json:"runtime_only"`
	Success        int64                     `json:"success"`
	Failed         int64                     `json:"failed"`
	NextRetryAfter time.Time                 `json:"next_retry_after"`
	Email          string                    `json:"email"`
	AccountType    string                    `json:"account_type"`
	Name           string                    `json:"name"`
	Path           string                    `json:"path"`
	ModelStates    map[string]hostModelState `json:"model_states"`
}

type hostModelState struct {
	Status         string    `json:"status"`
	Unavailable    bool      `json:"unavailable"`
	NextRetryAfter time.Time `json:"next_retry_after"`
}

type hostAuthGetRequest struct {
	AuthIndex string `json:"auth_index"`
}

type hostAuthGetResponse struct {
	JSON json.RawMessage `json:"json"`
}

type hostHTTPRequest struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body,omitempty"`
}

type hostHTTPResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers    map[string][]string `json:"Headers,omitempty"`
	Body       []byte              `json:"Body"`
}

func (h callbackHost) ListAuth() ([]quota.AuthFile, error) {
	result, err := h.invoke("host.auth.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp hostAuthListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("decode host.auth.list: %w", err)
	}
	out := make([]quota.AuthFile, 0, len(resp.Files))
	for _, f := range resp.Files {
		_ = f.Name
		_ = f.Path
		nextRetry := int64(0)
		if !f.NextRetryAfter.IsZero() {
			nextRetry = f.NextRetryAfter.Unix()
		}
		out = append(out, quota.AuthFile{
			AuthIndex:     f.AuthIndex,
			Provider:      f.Provider,
			Type:          f.Type,
			Status:        f.Status,
			Email:         f.Email,
			AccountType:   f.AccountType,
			Disabled:      f.Disabled,
			Unavailable:   f.Unavailable,
			RuntimeOnly:   f.RuntimeOnly,
			Success:       f.Success,
			Failed:        f.Failed,
			NextRetryUnix: nextRetry,
		})
	}
	return out, nil
}

func (h callbackHost) GetRuntime(authIndex string) (quota.RuntimeAuth, error) {
	result, err := h.invoke("host.auth.get_runtime", hostAuthGetRequest{AuthIndex: authIndex})
	if err != nil {
		return quota.RuntimeAuth{}, nil
	}
	var wrap struct {
		Auth hostAuthFile `json:"auth"`
	}
	if err := json.Unmarshal(result, &wrap); err != nil {
		var direct hostAuthFile
		if err2 := json.Unmarshal(result, &direct); err2 != nil {
			return quota.RuntimeAuth{}, nil
		}
		wrap.Auth = direct
	}
	_ = wrap.Auth.Name
	_ = wrap.Auth.Path
	out := quota.RuntimeAuth{Email: wrap.Auth.Email, AccountType: wrap.Auth.AccountType}
	for model, st := range wrap.Auth.ModelStates {
		status := st.Status
		if st.Unavailable {
			status = "unavailable"
		}
		if status == "" {
			status = "active"
		}
		out.Models = append(out.Models, quota.ModelAvailability{
			Provider:    wrap.Auth.Provider,
			AuthIndex:   authIndex,
			Email:       wrap.Auth.Email,
			Model:       model,
			Status:      status,
			Unavailable: st.Unavailable,
		})
	}
	return out, nil
}

func (h callbackHost) GetAuthJSON(authIndex string) ([]byte, error) {
	result, err := h.invoke("host.auth.get", hostAuthGetRequest{AuthIndex: authIndex})
	if err != nil {
		return nil, err
	}
	var resp hostAuthGetResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("decode host.auth.get: %w", err)
	}
	return resp.JSON, nil
}

func (h callbackHost) DoHTTP(req quota.HTTPRequest) (quota.HTTPResponse, error) {
	result, err := h.invoke("host.http.do", hostHTTPRequest{
		Method:  req.Method,
		URL:     req.URL,
		Headers: req.Headers,
		Body:    req.Body,
	})
	if err != nil {
		return quota.HTTPResponse{}, err
	}
	var resp hostHTTPResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return quota.HTTPResponse{}, fmt.Errorf("decode host.http.do: %w", err)
	}
	return quota.HTTPResponse{StatusCode: resp.StatusCode, Body: resp.Body}, nil
}

func (h callbackHost) invoke(method string, payload any) (json.RawMessage, error) {
	if h.call == nil {
		return nil, fmt.Errorf("host callback is not installed")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := h.call(method, raw)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(resp, &env); err != nil {
		return nil, fmt.Errorf("decode host envelope %s: %w", method, err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	return env.Result, nil
}
