package labels

import (
	"strings"
	"sync"
)

const (
	MaxProvider    = 64
	MaxModel       = 128
	MaxAuthIndex   = 64
	MaxWindow      = 64
	MaxStatus      = 32
	MaxEmail       = 128
	MaxAccountType = 32
	MaxProjectID   = 64
	MaxFormat      = 32
	MaxOutcome     = 16

	// Overflow replaces label values beyond a Limiter's capacity.
	Overflow = "other"
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
func Format(value string) string      { return Sanitize(value, MaxFormat) }

// Outcome maps request.complete outcomes onto a closed label set.
func Outcome(value string) string {
	switch v := strings.ToLower(strings.TrimSpace(value)); v {
	case "succeeded", "failed", "rejected", "canceled":
		return v
	default:
		return "unknown"
	}
}

// Limiter bounds the distinct values a label can take. Values seen first keep
// their own series; later new values collapse into Overflow.
type Limiter struct {
	max  int
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewLimiter(max int) *Limiter {
	return &Limiter{max: max, seen: map[string]struct{}{}}
}

// Value returns value while capacity remains or value was already admitted.
func (l *Limiter) Value(value string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.seen[value]; ok {
		return value
	}
	if len(l.seen) >= l.max {
		return Overflow
	}
	l.seen[value] = struct{}{}
	return value
}

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
