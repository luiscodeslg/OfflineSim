// Package update implements OfflineSim's signed self-update channel.
//
// Trust model (same as AzIdler's desktop client):
//   - An Ed25519 public key is baked into the app. Only the holder of the
//     matching private key can publish updates.
//   - An update is a manifest (JSON: build number + SHA-256 and size of the
//     new exe) and a detached Ed25519 signature over the exact manifest bytes.
//   - Nothing is installed unless the signature verifies, the build number is
//     higher than the running one (no rollbacks), and the downloaded exe
//     matches the signed size and hash.
//   - Downloads only follow https links to an allowlist of GitHub hosts.
package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AppID   = "offlinesim"
	MaxExe  = 200 << 20
	ExeName = "OfflineSim.exe"
)

// Manifest describes one published build.
type Manifest struct {
	App     string    `json:"app"`
	Build   int       `json:"build"`
	Version string    `json:"version"`
	Created time.Time `json:"created"`
	Notes   string    `json:"notes,omitempty"`
	Exe     File      `json:"exe"`
}

type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func ParsePublicKey(pemText []byte) (ed25519.PublicKey, error) {
	b, _ := pem.Decode(pemText)
	if b == nil {
		return nil, errors.New("no PEM block in public key")
	}
	k, err := x509.ParsePKIXPublicKey(b.Bytes)
	if err != nil {
		return nil, err
	}
	pk, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return pk, nil
}

func ParsePrivateKey(pemText []byte) (ed25519.PrivateKey, error) {
	b, _ := pem.Decode(pemText)
	if b == nil {
		return nil, errors.New("no PEM block in private key")
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return nil, err
	}
	pk, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not Ed25519")
	}
	return pk, nil
}

// Sign returns the base64 signature for manifest bytes.
func Sign(key ed25519.PrivateKey, manifest []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest))
}

// Verify checks the signature and parses the manifest. It does not check the build number.
func Verify(pub ed25519.PublicKey, manifest []byte, sigB64 string) (Manifest, error) {
	var m Manifest
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return m, errors.New("update rejected: malformed signature")
	}
	if !ed25519.Verify(pub, manifest, sig) {
		return m, errors.New("update rejected: bad signature")
	}
	if err := json.Unmarshal(manifest, &m); err != nil {
		return m, fmt.Errorf("update rejected: %w", err)
	}
	if m.App != AppID {
		return m, errors.New("update rejected: manifest is for a different app")
	}
	if m.Build < 1 {
		return m, errors.New("update rejected: bad build number")
	}
	if m.Exe.Name != ExeName || m.Exe.Size <= 0 || m.Exe.Size > MaxExe || len(m.Exe.SHA256) != 64 {
		return m, errors.New("update rejected: bad exe entry")
	}
	if _, err := hex.DecodeString(m.Exe.SHA256); err != nil {
		return m, errors.New("update rejected: bad exe hash")
	}
	return m, nil
}

// CheckFile verifies downloaded exe bytes against the signed entry.
func CheckFile(m Manifest, data []byte) error {
	if int64(len(data)) != m.Exe.Size {
		return fmt.Errorf("update rejected: size %d, expected %d", len(data), m.Exe.Size)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != m.Exe.SHA256 {
		return errors.New("update rejected: hash mismatch")
	}
	return nil
}

func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
