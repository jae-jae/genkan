package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

// EmbeddedPublicKey is the release signing key (base64 std, 32 bytes). The
// matching private key is the GENKAN_RELEASE_KEY secret of the release job.
const EmbeddedPublicKey = "j/YAUIXiDoyM9CHLn9cdGl65bJhXa5vjdbx8nfddl3Q="

// ParsePublicKey decodes a base64 (std) ed25519 public key.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("not a base64 ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}

// VerifySignature checks sigB64 (base64 std of an ed25519 signature, surrounding
// whitespace allowed) over the exact bytes of msg.
func VerifySignature(pub ed25519.PublicKey, msg, sigB64 []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("malformed signature")
	}
	if !ed25519.Verify(pub, msg, sig) {
		return fmt.Errorf("signature does not match")
	}
	return nil
}
