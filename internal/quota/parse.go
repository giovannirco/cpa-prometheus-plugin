package quota

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func ParseWindows(provider string, body []byte) []Window {
	if len(body) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	switch NormalizeProvider(provider) {
	case "claude":
		return parseClaude(payload)
	case "codex":
		return parseCodex(payload)
	case "antigravity":
		return parseAntigravity(payload)
	case "kimi":
		return parseKimi(payload)
	case "xai":
		return parseXAI(payload)
	case "gemini", "gemini-cli":
		return parseGemini(payload)
	default:
		return parseGeneric(payload)
	}
}

func NormalizeProvider(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "anthropic", "claude", "claude-web":
		return "claude"
	case "openai", "codex", "chatgpt":
		return "codex"
	case "gemini", "gemini-cli":
		return "gemini-cli"
	case "grok", "xai", "x-ai":
		return "xai"
	case "kimi", "moonshot", "kimi-coding":
		return "kimi"
	default:
		return p
	}
}

func SupportedProvider(provider string) bool {
	switch NormalizeProvider(provider) {
	case "claude", "codex", "antigravity", "kimi", "xai", "gemini-cli":
		return true
	default:
		return false
	}
}

func ParseResetCredits(provider string, body []byte) (ResetCreditInfo, bool) {
	if NormalizeProvider(provider) != "codex" || len(body) == 0 {
		return ResetCreditInfo{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ResetCreditInfo{}, false
	}
	block, _ := payload["rate_limit_reset_credits"].(map[string]any)
	if block == nil {
		if _, ok := payload["available_count"]; ok {
			block = payload
		}
	}
	if block == nil {
		return ResetCreditInfo{}, false
	}
	if block["available_count"] == nil {
		return ResetCreditInfo{}, false
	}
	info := ResetCreditInfo{Available: int(floatNumber(block["available_count"]))}
	if info.Available < 0 {
		info.Available = 0
	}
	var earliest int64
	credits, _ := block["credits"].([]any)
	for _, raw := range credits {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		status := strings.ToLower(firstString(item["status"]))
		if status != "" && status != "available" {
			continue
		}
		exp := unixTime(firstValue(item["expires_at"], item["expiresAt"]))
		if exp <= 0 {
			continue
		}
		if earliest == 0 || exp < earliest {
			earliest = exp
		}
	}
	info.ExpiresUnix = earliest
	return info, true
}

func parseClaude(payload map[string]any) []Window {
	out := make([]Window, 0, 4)
	for _, key := range []string{"five_hour", "seven_day"} {
		window, _ := payload[key].(map[string]any)
		if window == nil {
			continue
		}
		used := ratio(window["utilization"])
		out = append(out, Window{
			ID:             key,
			UsedRatio:      used,
			RemainingRatio: inverse(used),
			ResetUnix:      unixTime(window["resets_at"]),
		})
	}
	if limits, ok := payload["limits"].([]any); ok {
		for _, raw := range limits {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			id := firstString(item["type"], item["name"], item["id"])
			if id == "" {
				continue
			}
			used := ratio(firstValue(item["utilization"], item["used_percent"], item["used"]))
			out = append(out, Window{ID: slug(id), UsedRatio: used, RemainingRatio: inverse(used), ResetUnix: unixTime(firstValue(item["resets_at"], item["reset_at"]))})
		}
	}
	return out
}

func parseCodex(payload map[string]any) []Window {
	out := make([]Window, 0, 4)
	rate, _ := payload["rate_limit"].(map[string]any)
	if rate == nil {
		rate = payload
	}
	for _, item := range []struct{ id, key string }{{"five_hour", "primary_window"}, {"seven_day", "secondary_window"}} {
		w, _ := rate[item.key].(map[string]any)
		if w == nil {
			continue
		}
		used := percentRatio(w["used_percent"])
		if used == 0 {
			used = ratio(w["used_ratio"])
		}
		out = append(out, Window{ID: item.id, UsedRatio: used, RemainingRatio: inverse(used), ResetUnix: unixTime(firstValue(w["reset_at"], w["resets_at"], w["reset_after"]))})
	}
	return out
}

func parseAntigravity(payload map[string]any) []Window {
	out := make([]Window, 0)
	models, _ := payload["models"].([]any)
	if models == nil {
		if m, ok := payload["models"].(map[string]any); ok {
			for id, raw := range m {
				item, _ := raw.(map[string]any)
				if item == nil {
					continue
				}
				out = append(out, antigravityWindow(id, item))
			}
			return collapseAntigravityGroups(compactWindows(out))
		}
	}
	for _, raw := range models {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		id := firstString(item["name"], item["id"], item["model"])
		out = append(out, antigravityWindow(id, item))
	}
	return collapseAntigravityGroups(compactWindows(out))
}

func collapseAntigravityGroups(in []Window) []Window {
	groups := map[string]Window{}
	order := make([]string, 0, 2)
	for _, w := range in {
		id := antigravityGroupID(w.ID)
		existing, ok := groups[id]
		if !ok {
			w.ID = id
			groups[id] = w
			order = append(order, id)
			continue
		}
		if w.RemainingRatio < existing.RemainingRatio {
			w.ID = id
			groups[id] = w
		}
	}
	out := make([]Window, 0, len(order))
	for _, id := range order {
		out = append(out, groups[id])
	}
	return out
}

func antigravityGroupID(model string) string {
	s := strings.ToLower(model)
	if strings.Contains(s, "claude") || strings.Contains(s, "gpt") {
		return "claude_gpt_weekly"
	}
	return "gemini_weekly"
}

func antigravityWindow(id string, item map[string]any) Window {
	info, _ := item["quotaInfo"].(map[string]any)
	if info == nil {
		info = item
	}
	remaining := ratio(firstValue(info["remainingFraction"], info["remaining_fraction"], info["remaining"]))
	used := inverse(remaining)
	if remaining == 0 && info["used_percent"] != nil {
		used = percentRatio(info["used_percent"])
		remaining = inverse(used)
	}
	if id == "" {
		id = "model"
	}
	return Window{ID: slug(id), UsedRatio: used, RemainingRatio: remaining, ResetUnix: unixTime(firstValue(info["resetTime"], info["reset_time"], info["resets_at"]))}
}

func parseGemini(payload map[string]any) []Window {
	buckets, _ := payload["buckets"].([]any)
	out := make([]Window, 0, len(buckets))
	for _, raw := range buckets {
		b, _ := raw.(map[string]any)
		if b == nil {
			continue
		}
		id := firstString(b["modelId"], b["model_id"], b["id"])
		if id == "" {
			continue
		}
		remaining := ratio(firstValue(b["remainingFraction"], b["remaining_fraction"]))
		out = append(out, Window{ID: slug(id), UsedRatio: inverse(remaining), RemainingRatio: remaining, ResetUnix: unixTime(firstValue(b["resetTime"], b["reset_time"]))})
	}
	return out
}

func parseKimi(payload map[string]any) []Window {
	out := make([]Window, 0)
	if limits, ok := payload["limits"].([]any); ok {
		for i, raw := range limits {
			item, _ := raw.(map[string]any)
			if item == nil {
				continue
			}
			id := firstString(item["type"], item["name"], item["id"])
			if id == "" {
				id = "limit_" + strconv.Itoa(i)
			}
			used, remaining := usedRemaining(item)
			out = append(out, Window{ID: slug(id), UsedRatio: used, RemainingRatio: remaining, ResetUnix: unixTime(firstValue(item["resets_at"], item["reset_at"], item["resetTime"]))})
		}
	}
	for _, key := range []string{"five_hour", "weekly", "seven_day"} {
		w, _ := payload[key].(map[string]any)
		if w == nil {
			continue
		}
		used, remaining := usedRemaining(w)
		out = append(out, Window{ID: key, UsedRatio: used, RemainingRatio: remaining, ResetUnix: unixTime(firstValue(w["resets_at"], w["reset_at"]))})
	}
	return compactWindows(out)
}

func parseXAI(payload map[string]any) []Window {
	out := make([]Window, 0, 4)
	config, _ := payload["config"].(map[string]any)
	period := map[string]any{}
	if config != nil {
		if p, ok := config["currentPeriod"].(map[string]any); ok {
			period = p
		}
	}
	reset := unixTime(firstValue(
		payload["resets_at"], payload["period_end"], payload["billingPeriodEnd"],
		configValue(config, "billingPeriodEnd"),
		period["end"],
	))
	if config != nil && config["creditUsagePercent"] != nil {
		used := percentRatio(config["creditUsagePercent"])
		out = append(out, Window{ID: "weekly", UsedRatio: used, RemainingRatio: inverse(used), ResetUnix: reset})
	}
	if credits, ok := payload["credits"].(map[string]any); ok {
		used, remaining := usedRemaining(credits)
		out = append(out, Window{ID: "credits", UsedRatio: used, RemainingRatio: remaining, ResetUnix: unixTime(firstValue(credits["resets_at"], credits["reset_at"], reset))})
	}
	for _, item := range []struct{ id, key string }{
		{"weekly", "weekly"},
		{"grok_build", "grok_build"},
		{"grok_build", "grokbuild"},
		{"grok_build", "grokBuild"},
	} {
		w, _ := payload[item.key].(map[string]any)
		if w == nil && config != nil {
			w, _ = config[item.key].(map[string]any)
		}
		if w == nil {
			continue
		}
		used, remaining := usedRemaining(w)
		if used == 0 && remaining == 0 && w["used_percent"] == nil && w["utilization"] == nil {
			continue
		}
		out = append(out, Window{ID: item.id, UsedRatio: used, RemainingRatio: remaining, ResetUnix: unixTime(firstValue(w["resets_at"], w["reset_at"], reset))})
	}
	if len(out) == 0 {
		used, remaining := usedRemaining(payload)
		if used != 0 || remaining != 0 {
			out = append(out, Window{ID: "credits", UsedRatio: used, RemainingRatio: remaining, ResetUnix: reset})
		}
	}
	return compactWindows(out)
}

func configValue(config map[string]any, key string) any {
	if config == nil {
		return nil
	}
	return config[key]
}

func parseGeneric(payload map[string]any) []Window {
	used, remaining := usedRemaining(payload)
	if used == 0 && remaining == 0 {
		return nil
	}
	return []Window{{ID: "default", UsedRatio: used, RemainingRatio: remaining, ResetUnix: unixTime(firstValue(payload["resets_at"], payload["reset_at"]))}}
}

func usedRemaining(m map[string]any) (used, remaining float64) {
	if m == nil {
		return 0, 0
	}
	if m["utilization"] != nil {
		used = ratio(m["utilization"])
		return used, inverse(used)
	}
	if m["used_percent"] != nil {
		used = percentRatio(m["used_percent"])
		return used, inverse(used)
	}
	if m["remainingFraction"] != nil || m["remaining_fraction"] != nil {
		remaining = ratio(firstValue(m["remainingFraction"], m["remaining_fraction"]))
		return inverse(remaining), remaining
	}
	limit := floatNumber(firstValue(m["limit"], m["total"], m["quota"]))
	usedN := floatNumber(firstValue(m["used"], m["consumed"]))
	remainN := floatNumber(firstValue(m["remaining"], m["remain"]))
	if limit > 0 {
		if remainN > 0 && usedN == 0 {
			usedN = limit - remainN
		}
		if usedN > 0 && remainN == 0 {
			remainN = math.Max(limit-usedN, 0)
		}
		return clamp(usedN / limit), clamp(remainN / limit)
	}
	return 0, 0
}

func compactWindows(in []Window) []Window {
	seen := map[string]int{}
	out := make([]Window, 0, len(in))
	for _, w := range in {
		if w.ID == "" {
			continue
		}
		if i, ok := seen[w.ID]; ok {
			out[i] = w
			continue
		}
		seen[w.ID] = len(out)
		out = append(out, w)
	}
	return out
}

func ratio(v any) float64 {
	n := floatNumber(v)
	if n > 1 && n <= 100 {
		return clamp(n / 100)
	}
	return clamp(n)
}

func percentRatio(v any) float64 {
	return clamp(floatNumber(v) / 100)
}

func inverse(used float64) float64 { return clamp(1 - used) }

func clamp(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func floatNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f
	default:
		return 0
	}
}

func unixTime(v any) int64 {
	switch t := v.(type) {
	case float64:
		if t > 1e12 {
			return int64(t / 1000)
		}
		return int64(t)
	case int64:
		return t
	case json.Number:
		i, _ := t.Int64()
		return i
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if unix, err := strconv.ParseInt(s, 10, 64); err == nil && unix > 0 {
			return unix
		}
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return ts.Unix()
		}
		if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return ts.Unix()
		}
	}
	return 0
}

func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func firstValue(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "window"
	}
	return b.String()
}
