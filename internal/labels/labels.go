package labels

import "strings"

const (
	MaxProvider  = 64
	MaxModel     = 128
	MaxAuthIndex = 64
	MaxWindow    = 64
	MaxStatus    = 32
)

func Sanitize(value string, max int) string {
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

func Provider(value string) string  { return Sanitize(value, MaxProvider) }
func Model(value string) string     { return Sanitize(value, MaxModel) }
func AuthIndex(value string) string { return Sanitize(value, MaxAuthIndex) }
func Window(value string) string    { return Sanitize(value, MaxWindow) }
func Status(value string) string    { return Sanitize(value, MaxStatus) }
