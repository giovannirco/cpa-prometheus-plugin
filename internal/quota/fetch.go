package quota

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AuthFile struct {
	AuthIndex       string
	Provider        string
	Type            string
	Status          string
	Email           string
	AccountType     string
	Disabled        bool
	Unavailable     bool
	RuntimeOnly     bool
	Success         int64
	Failed          int64
	NextRetryUnix   int64
	LastRefreshUnix int64
	UpdatedAtUnix   int64
	ProjectID       string
}

type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string][]string
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Body       []byte
}

type Host interface {
	ListAuth() ([]AuthFile, error)
	GetAuthJSON(authIndex string) ([]byte, error)
	GetRuntime(authIndex string) (RuntimeAuth, error)
	DoHTTP(HTTPRequest) (HTTPResponse, error)
}

func Poll(host Host, cfg Config) ([]Account, []Credential, error) {
	if host == nil {
		return nil, nil, fmt.Errorf("host is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultRefreshInterval
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 20 * time.Second
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 4
	}
	files, err := host.ListAuth()
	if err != nil {
		return nil, nil, err
	}
	creds := make([]Credential, 0, len(files))
	jobs := make([]AuthFile, 0, len(files))
	for _, file := range files {
		status := file.Status
		if file.Disabled {
			status = "disabled"
		} else if file.Unavailable {
			status = "unavailable"
		} else if strings.TrimSpace(status) == "" {
			status = "active"
		}
		provider := NormalizeProvider(firstNonEmpty(file.Provider, file.Type))
		email := file.Email
		accountType := file.AccountType
		var models []ModelAvailability
		if rt, err := host.GetRuntime(file.AuthIndex); err == nil {
			if email == "" {
				email = rt.Email
			}
			if accountType == "" {
				accountType = rt.AccountType
			}
			models = rt.Models
		}
		creds = append(creds, Credential{
			Provider:        provider,
			AuthIndex:       file.AuthIndex,
			Status:          status,
			Email:           email,
			AccountType:     accountType,
			Disabled:        file.Disabled,
			Unavailable:     file.Unavailable,
			RuntimeOnly:     file.RuntimeOnly,
			Success:         file.Success,
			Failed:          file.Failed,
			NextRetryUnix:   file.NextRetryUnix,
			LastRefreshUnix: file.LastRefreshUnix,
			UpdatedAtUnix:   file.UpdatedAtUnix,
			ProjectID:       file.ProjectID,
			Models:          models,
		})
		file.Email = email
		file.AccountType = accountType
		if file.AuthIndex == "" || file.RuntimeOnly {
			continue
		}
		if file.Disabled && !cfg.IncludeDisabled {
			continue
		}
		jobs = append(jobs, file)
	}
	accounts := make([]Account, len(jobs))
	sem := make(chan struct{}, cfg.MaxConcurrency)
	var wg sync.WaitGroup
	for i, file := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, file AuthFile) {
			defer wg.Done()
			defer func() { <-sem }()
			accounts[i] = fetchOne(host, file)
		}(i, file)
	}
	wg.Wait()
	return accounts, creds, nil
}

func fetchOne(host Host, file AuthFile) Account {
	provider := NormalizeProvider(firstNonEmpty(file.Provider, file.Type))
	account := Account{
		Provider:    provider,
		AuthIndex:   file.AuthIndex,
		Status:      file.Status,
		Email:       file.Email,
		AccountType: file.AccountType,
		Supported:   SupportedProvider(provider),
	}
	if !account.Supported {
		return account
	}
	raw, err := host.GetAuthJSON(file.AuthIndex)
	if err != nil {
		account.Error = err.Error()
		return account
	}
	token := lookupString(raw, "access_token")
	if token == "" {
		token = lookupString(raw, "token")
	}
	if token == "" {
		account.Error = "credential_incomplete"
		return account
	}
	req, err := providerRequest(provider, token, raw)
	if err != nil {
		account.Error = err.Error()
		return account
	}
	resp, err := host.DoHTTP(req)
	if err != nil {
		account.Error = err.Error()
		return account
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		account.Error = fmt.Sprintf("upstream HTTP %d", resp.StatusCode)
		return account
	}
	account.Windows = ParseWindows(provider, resp.Body)
	account.FetchedAt = time.Now().UTC()
	return account
}

func providerRequest(provider, token string, raw []byte) (HTTPRequest, error) {
	headers := map[string][]string{
		"Authorization": {"Bearer " + token},
		"Accept":        {"application/json"},
	}
	switch NormalizeProvider(provider) {
	case "claude":
		headers["anthropic-beta"] = []string{"oauth-2025-04-20"}
		headers["User-Agent"] = []string{"claude-code/2.1.0"}
		return HTTPRequest{Method: http.MethodGet, URL: "https://api.anthropic.com/api/oauth/usage", Headers: headers}, nil
	case "codex":
		accountID := lookupString(raw, "account_id")
		if accountID == "" {
			accountID = lookupString(raw, "chatgpt_account_id")
		}
		if accountID == "" {
			accountID = accountIDFromJWT(lookupString(raw, "id_token"))
		}
		if accountID == "" {
			return HTTPRequest{}, fmt.Errorf("credential_incomplete")
		}
		headers["Chatgpt-Account-Id"] = []string{accountID}
		headers["User-Agent"] = []string{"codex_cli_rs/0.76.0 (linux; amd64)"}
		return HTTPRequest{Method: http.MethodGet, URL: "https://chatgpt.com/backend-api/wham/usage", Headers: headers}, nil
	case "antigravity":
		projectID := lookupString(raw, "project_id")
		if projectID == "" {
			return HTTPRequest{}, fmt.Errorf("credential_incomplete")
		}
		body, _ := json.Marshal(map[string]any{"project": projectID})
		headers["Content-Type"] = []string{"application/json"}
		headers["User-Agent"] = []string{"antigravity/1.21.9 linux/amd64"}
		return HTTPRequest{Method: http.MethodPost, URL: "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels", Headers: headers, Body: body}, nil
	case "kimi":
		headers["User-Agent"] = []string{"kimi-cli/1.0"}
		return HTTPRequest{Method: http.MethodGet, URL: "https://api.kimi.com/coding/v1/usages", Headers: headers}, nil
	case "xai":
		headers["x-xai-token-auth"] = []string{"xai-grok-cli"}
		headers["x-grok-client-version"] = []string{"0.2.91"}
		headers["User-Agent"] = []string{"grok-shell/0.2.91"}
		return HTTPRequest{Method: http.MethodGet, URL: "https://cli-chat-proxy.grok.com/v1/billing?format=credits", Headers: headers}, nil
	default:
		return HTTPRequest{}, fmt.Errorf("unsupported")
	}
}

func lookupString(raw []byte, key string) string {
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return lookupAny(doc, key)
}

func lookupAny(value any, key string) string {
	switch v := value.(type) {
	case map[string]any:
		if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
		for _, child := range v {
			if s := lookupAny(child, key); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range v {
			if s := lookupAny(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func accountIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var doc map[string]any
	if json.Unmarshal(payload, &doc) != nil {
		return ""
	}
	if s, _ := doc["chatgpt_account_id"].(string); s != "" {
		return s
	}
	if auth, ok := doc["https://api.openai.com/auth"].(map[string]any); ok {
		if s, _ := auth["chatgpt_account_id"].(string); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
