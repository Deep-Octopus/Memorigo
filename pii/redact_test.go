package pii

import "testing"

func TestRedactHighConfidencePatterns(t *testing.T) {
	in := "Mail ada@example.com, call 13800138000 or +14155552671, card 4111-1111-1111-1111."
	got := Redact(in)
	for _, secret := range []string{"ada@example.com", "13800138000", "+14155552671", "4111-1111-1111-1111"} {
		if contains(got, secret) {
			t.Fatalf("secret %q survived in %q", secret, got)
		}
	}
	for _, token := range []string{EmailToken, PhoneToken, CardToken} {
		if !contains(got, token) {
			t.Fatalf("missing %s in %q", token, got)
		}
	}
	if again := Redact(got); again != got {
		t.Fatalf("not idempotent: %q", again)
	}
}

func TestRedactLeavesOrdinaryNumbers(t *testing.T) {
	in := "I moved in 2024 and the order id is 100200300."
	if got := Redact(in); got != in {
		t.Fatalf("over-redacted: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
