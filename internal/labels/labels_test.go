package labels

import (
	"strings"
	"testing"
)

func TestSanitizeDropsPIIShapedValuesAndUnknownEmpty(t *testing.T) {
	if got := Sanitize("", 8); got != "unknown" {
		t.Fatalf("empty = %q", got)
	}
	got := Sanitize("User@Example.com", 64)
	if stringsContainsAt(got) {
		t.Fatalf("email-shaped value leaked: %q", got)
	}
	if got != "user_example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestEmailKeepsAtSign(t *testing.T) {
	got := Email("Gio@Example.com")
	if got != "gio@example.com" {
		t.Fatalf("got %q", got)
	}
	if Email("") != "unknown" {
		t.Fatalf("empty email should be unknown")
	}
}

func TestEmailRejectsTokenAndPath(t *testing.T) {
	for _, in := range []string{
		"sk-abcdefghijklmnopqrstuvwxyz",
		"ghp_abcdefghijklmnopqrstuvwx",
		"github_pat_abcdefghijklmnopqrstuvwx",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.aaa",
		"/root/.cli-proxy-api/gio.json",
		"Bearer tok-secret",
	} {
		if got := Email(in); got != "unknown" {
			t.Fatalf("Email(%q) = %q, want unknown", in, got)
		}
	}
	if got := Email("gio@example.com"); got != "gio@example.com" {
		t.Fatalf("real email dropped: %q", got)
	}
}

func TestSanitizeRejectsTokenAndPath(t *testing.T) {
	if got := Sanitize("sk-abcdefghijklmnopqrstuvwxyz", 64); got != "unknown" {
		t.Fatalf("Sanitize token = %q", got)
	}
	if got := Sanitize("/root/.cli-proxy-api/secret.json", 64); got != "unknown" {
		t.Fatalf("Sanitize path = %q", got)
	}
}

func FuzzEmail(f *testing.F) {
	f.Add("gio@example.com")
	f.Add("sk-secret-value")
	f.Add("/root/.cli-proxy-api/x.json")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		got := Email(s)
		if len(got) > MaxEmail {
			t.Fatalf("len %d", len(got))
		}
		lower := strings.ToLower(got)
		if strings.HasPrefix(lower, "sk-") || strings.HasPrefix(lower, "ghp_") || strings.Contains(got, "/") {
			t.Fatalf("secret/path leaked %q -> %q", s, got)
		}
	})
}

func FuzzSanitize(f *testing.F) {
	f.Add("xai")
	f.Add("sk-secret")
	f.Add("/etc/passwd")
	f.Fuzz(func(t *testing.T, s string) {
		got := Sanitize(s, MaxProvider)
		if len(got) > MaxProvider {
			t.Fatalf("len %d", len(got))
		}
		if strings.Contains(got, "/") || strings.HasPrefix(got, "sk-") {
			t.Fatalf("leaked %q -> %q", s, got)
		}
	})
}

func TestModelKeepsProviderNamespace(t *testing.T) {
	for _, in := range []string{
		"opencode-go/deepseek-v4.1-flash",
		"agy/gemini-4-neon-high",
		"zai/glm-5.3",
		"deepseek-v4.1-flash",
	} {
		if got := Model(in); got != in {
			t.Fatalf("Model(%q) = %q, want unchanged", in, got)
		}
	}
	if got := Model("OpenCode-Go/DeepSeek-V4.1-Flash"); got != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("Model case folding = %q", got)
	}
}

func TestModelRejectsPathsURLsAndTokens(t *testing.T) {
	for _, in := range []string{
		"",
		"/root/.cli-proxy-api/secret.json",
		"~/secrets/gio.json",
		`C:\Users\gio\secret.json`,
		"https://example.com/v1/models",
		"state/oauth/secret.json",
		"sk-abcdefghijklmnopqrstuvwxyz",
		"Bearer tok-secret",
	} {
		if got := Model(in); got != "unknown" {
			t.Fatalf("Model(%q) = %q, want unknown", in, got)
		}
	}
}

func TestSanitizeAllowsOneInteriorSeparator(t *testing.T) {
	if got := Sanitize("state/oauth.json", 64); got != "state_oauth.json" {
		t.Fatalf("single separator = %q", got)
	}
	for _, in := range []string{"/root/secret.json", "~/secret.json", "state/oauth/secret.json", `C:\secret.json`, "https://example.com/x"} {
		if got := Sanitize(in, 64); got != "unknown" {
			t.Fatalf("Sanitize(%q) = %q, want unknown", in, got)
		}
	}
}

func TestModelTruncates(t *testing.T) {
	if got := sanitize("aaaa/bbbb", 4, "/"); got != "aaaa" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a", MaxModel+10)
	if got := Model(long); len(got) != MaxModel {
		t.Fatalf("len %d", len(got))
	}
}

func FuzzModel(f *testing.F) {
	f.Add("opencode-go/deepseek-v4.1-flash")
	f.Add("agy/gemini-4-neon-high")
	f.Add("/etc/passwd")
	f.Add("sk-secret-value")
	f.Fuzz(func(t *testing.T, s string) {
		got := Model(s)
		if len(got) > MaxModel {
			t.Fatalf("len %d", len(got))
		}
		if strings.Contains(got, `\`) || strings.Contains(got, "://") || strings.HasPrefix(got, "/") {
			t.Fatalf("path or URL leaked %q -> %q", s, got)
		}
		if strings.Count(got, "/") > 1 {
			t.Fatalf("nested path leaked %q -> %q", s, got)
		}
		if strings.HasPrefix(got, "sk-") || strings.HasPrefix(got, "bearer ") {
			t.Fatalf("token leaked %q -> %q", s, got)
		}
	})
}

func TestSanitizeTruncates(t *testing.T) {
	got := Sanitize("abcdefghij", 4)
	if got != "abcd" {
		t.Fatalf("got %q", got)
	}
}

func stringsContainsAt(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			return true
		}
	}
	return false
}
