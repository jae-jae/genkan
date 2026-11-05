// Command relsign creates and checks the ed25519 signature of Genkan release
// checksums. It uses only the standard library so the release workflow can run
// it with plain `go run ./tools/relsign`.
//
//	relsign keygen [-out PREFIX]
//	relsign sign   <file>                       (private key from $GENKAN_RELEASE_KEY)
//	relsign verify <file> <sigfile> <pubkey-b64>
//
// Keys and signatures are base64 (std encoding). The private key is the 64-byte
// ed25519 private key, the public key the 32-byte one. A signature file holds
// the base64 signature followed by a newline.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

const keyEnv = "GENKAN_RELEASE_KEY"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "relsign:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return errors.New("usage: relsign keygen [-out PREFIX] | sign <file> | verify <file> <sigfile> <pubkey-b64>")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:], stdout)
	case "sign":
		if len(args) != 2 {
			return errors.New("usage: relsign sign <file>")
		}
		return sign(args[1], getenv(keyEnv))
	case "verify":
		if len(args) != 4 {
			return errors.New("usage: relsign verify <file> <sigfile> <pubkey-b64>")
		}
		if err := verify(args[1], args[2], args[3]); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "OK")
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// keygen prints a new key pair. With -out PREFIX the private key goes to
// PREFIX.key (mode 0600, never overwritten) and the public key to PREFIX.pub;
// only the public key is printed then.
func keygen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "", "write PREFIX.key (0600) and PREFIX.pub instead of printing the private key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: relsign keygen [-out PREFIX]")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privB64 := base64.StdEncoding.EncodeToString(priv)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	if *out == "" {
		fmt.Fprintf(stdout, "private (secret %s): %s\npublic (embed in internal/update): %s\n", keyEnv, privB64, pubB64)
		return nil
	}
	f, err := os.OpenFile(*out+".key", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(privB64 + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(*out+".pub", []byte(pubB64+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "private key written to %s.key (keep it secret)\npublic (embed in internal/update): %s\n", *out, pubB64)
	return nil
}

func parsePrivate(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("%s is not set", keyEnv)
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64: %w", keyEnv, err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d", keyEnv, ed25519.PrivateKeySize, len(raw))
	}
	priv := ed25519.PrivateKey(raw)
	// the last 32 bytes are the public key; a mismatch means a corrupted key
	if !priv.Public().(ed25519.PublicKey).Equal(ed25519.NewKeyFromSeed(priv.Seed()).Public()) {
		return nil, fmt.Errorf("%s is inconsistent (public half does not match the seed)", keyEnv)
	}
	return priv, nil
}

func sign(file, keyB64 string) error {
	priv, err := parsePrivate(keyB64)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data))
	return os.WriteFile(file+".sig", []byte(sig+"\n"), 0o644)
}

func verify(file, sigFile, pubB64 string) error {
	pubRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return errors.New("public key must be base64 of 32 bytes")
	}
	sigText, err := os.ReadFile(sigFile)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return fmt.Errorf("signature is not valid base64: %w", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(ed25519.PublicKey(pubRaw), data, sig) {
		return errors.New("signature does not match")
	}
	return nil
}
