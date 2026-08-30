package labels

import "testing"

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
