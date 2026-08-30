package labels

import "strings"

const (
	MaxProvider    = 64
	MaxModel       = 128
	MaxAuthIndex   = 64
	MaxWindow      = 64
	MaxStatus      = 32
	MaxEmail       = 128
	MaxAccountType = 32
	MaxProjectID   = 64
)

func secretLike(value string) bool {
	s := strings.ToLower(strings.TrimSpace(value))
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, `/\`) || strings.Contains(s, "://") {
		return true
	}
	for _, prefix := range []string{
		"sk-", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "xai-", "eyj", "bearer ",
	} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return strings.Contains(s, "-----begin")
}

func Sanitize(value string, max int) string {
	if secretLike(value) {
		return "unknown"
	}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	if out == "" {
		return "unknown"
	}
	return out
}

func Provider(value string) string    { return Sanitize(value, MaxProvider) }
func Model(value string) string       { return Sanitize(value, MaxModel) }
func AuthIndex(value string) string   { return Sanitize(value, MaxAuthIndex) }
func Window(value string) string      { return Sanitize(value, MaxWindow) }
func Status(value string) string      { return Sanitize(value, MaxStatus) }
func AccountType(value string) string { return Sanitize(value, MaxAccountType) }
func ProjectID(value string) string   { return Sanitize(value, MaxProjectID) }

func Email(value string) string {
	if secretLike(value) {
		return "unknown"
	}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '@', r == '.', r == '_', r == '-', r == '+':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if MaxEmail > 0 && len(out) > MaxEmail {
		out = out[:MaxEmail]
	}
	if out == "" {
		return "unknown"
	}
	return out
}
