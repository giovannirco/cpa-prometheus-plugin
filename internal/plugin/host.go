package plugin

import (
	"encoding/json"
	"fmt"

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
	AuthIndex   string `json:"auth_index"`
	Provider    string `json:"provider"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Disabled    bool   `json:"disabled"`
	Unavailable bool   `json:"unavailable"`
	RuntimeOnly bool   `json:"runtime_only"`
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
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"body"`
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
		out = append(out, quota.AuthFile{
			AuthIndex:   f.AuthIndex,
			Provider:    f.Provider,
			Type:        f.Type,
			Status:      f.Status,
			Disabled:    f.Disabled,
			Unavailable: f.Unavailable,
			RuntimeOnly: f.RuntimeOnly,
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
