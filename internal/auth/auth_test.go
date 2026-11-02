package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSession(t *testing.T) {
	now := time.Now()
	v := SignSession("secret", now.Add(time.Hour))
	if !VerifySession("secret", v, now) {
		t.Fatal("valid session rejected")
	}
	if VerifySession("other", v, now) {
		t.Fatal("wrong secret accepted")
	}
	if VerifySession("secret", v, now.Add(2*time.Hour)) {
		t.Fatal("expired accepted")
	}
	ts, sig, _ := strings.Cut(v, ".")
	forged := "9999999999." + sig
	if VerifySession("secret", forged, now) {
		t.Fatal("forged expiry accepted")
	}
	_ = ts
	for _, bad := range []string{"", ".", "abc", "123.", "x.y", v + "0", "secret"} {
		if VerifySession("secret", bad, now) {
			t.Errorf("accepted %q", bad)
		}
	}
	if VerifySession("", v, now) {
		t.Fatal("empty secret accepted")
	}
}

func TestPassword(t *testing.T) {
	h, err := HashPassword("hunter22")
	if err != nil || !CheckPassword(h, "hunter22") || CheckPassword(h, "hunter23") || CheckPassword("", "") {
		t.Fatal("password roundtrip")
	}
	if ValidatePassword("12345") == "" || ValidatePassword("123456") != "" || ValidatePassword(strings.Repeat("a", 73)) == "" {
		t.Fatal("validate")
	}
}

func TestToken(t *testing.T) {
	plain := NewToken()
	if !strings.HasPrefix(plain, "gk_") || len(plain) != 35 {
		t.Fatalf("token format %q", plain)
	}
	for _, r := range plain[3:] {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz234567", r) {
			t.Fatalf("non-base32 char %q in %q", r, plain)
		}
	}
	if NewToken() == plain {
		t.Fatal("tokens must be random")
	}
}

func TestLimiter(t *testing.T) {
	cur := time.Unix(1000, 0)
	l := NewLimiter(5, time.Minute)
	l.Now = func() time.Time { return cur }
	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow("1.1.1.1"); !ok {
			t.Fatalf("blocked early at %d", i)
		}
		l.Fail("1.1.1.1")
		cur = cur.Add(time.Second)
	}
	ok, wait := l.Allow("1.1.1.1")
	if ok || wait <= 0 || wait > time.Minute {
		t.Fatalf("expected block, wait=%v", wait)
	}
	if ok, _ := l.Allow("2.2.2.2"); !ok {
		t.Fatal("other IP blocked")
	}
	cur = cur.Add(time.Minute)
	if ok, _ := l.Allow("1.1.1.1"); !ok {
		t.Fatal("still blocked after window")
	}
	l.Fail("3.3.3.3")
	l.Reset("3.3.3.3")
	for i := 0; i < 4; i++ {
		l.Fail("3.3.3.3")
	}
	if ok, _ := l.Allow("3.3.3.3"); !ok {
		t.Fatal("reset did not clear")
	}
}
