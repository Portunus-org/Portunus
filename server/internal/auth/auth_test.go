package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	for pw, want := range map[string]bool{
		"correct horse battery":  true,
		"correct horse battery ": false,
		"":                       false,
	} {
		got, err := VerifyPassword(hash, pw)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("VerifyPassword(%q) = %v, want %v", pw, got, want)
		}
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	for _, h := range []string{"", "plain", "$bcrypt$x$y$z$w", "$argon2id$v=19$m=1,t=1,p=1$!!$!!"} {
		if ok, err := VerifyPassword(h, "x"); ok || err == nil {
			t.Errorf("VerifyPassword(%q) = %v, %v; want false, error", h, ok, err)
		}
	}
}

func TestGeneratePassword(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		pw := GeneratePassword()
		if len(pw) != 20 || strings.ContainsAny(pw, "0O1lI") {
			t.Fatalf("bad generated password %q", pw)
		}
		if err := ValidatePassword(pw); err != nil {
			t.Fatalf("generated password fails validation: %v", err)
		}
		if seen[pw] {
			t.Fatal("duplicate generated password")
		}
		seen[pw] = true
	}
}

func TestLoginLimiterUserLockout(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := NewLoginLimiter()
	l.now = func() time.Time { return now }

	for i := range userFreeFailures {
		if ok, _ := l.Allow("10.0.0.1", "admin"); !ok {
			t.Fatalf("attempt %d blocked too early", i)
		}
		l.Failure("admin")
	}
	ok, wait := l.Allow("10.0.0.2", "admin")
	if ok || wait != userBaseLockout {
		t.Fatalf("after %d failures: ok=%v wait=%v, want locked for %v", userFreeFailures, ok, wait, userBaseLockout)
	}
	// A different account is unaffected.
	if ok, _ := l.Allow("10.0.0.2", "other"); !ok {
		t.Fatal("lockout leaked to another username")
	}
	now = now.Add(userBaseLockout)
	if ok, _ := l.Allow("10.0.0.3", "admin"); !ok {
		t.Fatal("lockout did not expire")
	}
	l.Success("admin")
	if _, locked := l.users["admin"]; locked {
		t.Fatal("success did not clear failures")
	}
}

func TestLoginLimiterIPBucket(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := NewLoginLimiter()
	l.now = func() time.Time { return now }

	for i := range ipBurst {
		if ok, _ := l.Allow("10.0.0.1", "u"+string(rune('a'+i))); !ok {
			t.Fatalf("attempt %d within burst was blocked", i)
		}
	}
	if ok, _ := l.Allow("10.0.0.1", "zz"); ok {
		t.Fatal("burst exceeded but attempt allowed")
	}
	now = now.Add(time.Duration(1/ipRefillPerSec) * time.Second)
	if ok, _ := l.Allow("10.0.0.1", "zz"); !ok {
		t.Fatal("bucket did not refill")
	}
}
