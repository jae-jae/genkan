package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestKeygenSignVerify(t *testing.T) {
	dir := t.TempDir()
	prefix := filepath.Join(dir, "rel")
	var out bytes.Buffer
	if err := run([]string{"keygen", "-out", prefix}, &out, env(nil)); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(prefix + ".key")
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("private key mode = %v, want 0600", st.Mode().Perm())
	}
	if err := run([]string{"keygen", "-out", prefix}, &out, env(nil)); err == nil {
		t.Error("keygen overwrote an existing key")
	}
	priv, _ := os.ReadFile(prefix + ".key")
	pub, _ := os.ReadFile(prefix + ".pub")
	if strings.Contains(out.String(), strings.TrimSpace(string(priv))) {
		t.Error("private key was printed although -out was given")
	}

	file := filepath.Join(dir, "checksums.txt")
	os.WriteFile(file, []byte("abc  genkan_1.0.0_linux_amd64.tar.gz\n"), 0o644)
	e := env(map[string]string{keyEnv: string(priv)}) // trailing newline is tolerated
	if err := run([]string{"sign", file}, &out, e); err != nil {
		t.Fatal(err)
	}
	sig, _ := os.ReadFile(file + ".sig")
	if !strings.HasSuffix(string(sig), "\n") || strings.Count(string(sig), "\n") != 1 {
		t.Errorf("signature file = %q, want one base64 line", sig)
	}
	pk := strings.TrimSpace(string(pub))
	if err := run([]string{"verify", file, file + ".sig", pk}, &out, e); err != nil {
		t.Fatalf("verify: %v", err)
	}

	os.WriteFile(file, []byte("tampered\n"), 0o644)
	if err := run([]string{"verify", file, file + ".sig", pk}, &out, e); err == nil {
		t.Error("verify accepted a tampered file")
	}
}

func TestSignRejectsBadKey(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	os.WriteFile(file, []byte("x"), 0o644)
	for name, key := range map[string]string{
		"missing":   "",
		"not b64":   "!!!",
		"too short": "AAAA",
	} {
		err := run([]string{"sign", file}, &bytes.Buffer{}, env(map[string]string{keyEnv: key}))
		if err == nil || !strings.Contains(err.Error(), keyEnv) {
			t.Errorf("%s: err = %v, want an error naming %s", name, err, keyEnv)
		}
		if _, err := os.Stat(file + ".sig"); err == nil {
			t.Errorf("%s: a signature file was written", name)
		}
	}
}
