// Writes and signs the update manifest for a built OfflineSim.exe.
//
//	OFFLINESIM_SIGNING_KEY="<PEM text>" go run ./tools/sign -exe dist/OfflineSim.exe -build 12 -version 1.0.12 -out dist
//	go run ./tools/sign -key path/to/private.pem -exe ... -build ... -out dist
//
// Produces <out>/manifest.json and <out>/manifest.sig. Refuses to sign with a
// key that doesn't match update/public-key.pem (the key baked into the app).
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"offlinesim/internal/update"
)

func main() {
	keyFile := flag.String("key", "", "private key file (default: $OFFLINESIM_SIGNING_KEY)")
	exe := flag.String("exe", "dist/OfflineSim.exe", "built exe")
	build := flag.Int("build", 0, "build number (must increase with every release)")
	version := flag.String("version", "", "display version")
	notes := flag.String("notes", "", "release notes")
	out := flag.String("out", "dist", "output folder")
	flag.Parse()

	pemText := []byte(os.Getenv("OFFLINESIM_SIGNING_KEY"))
	if *keyFile != "" {
		var err error
		pemText, err = os.ReadFile(*keyFile)
		check(err)
	}
	if len(pemText) == 0 {
		fail("no signing key: set OFFLINESIM_SIGNING_KEY or pass -key")
	}
	if *build < 1 {
		fail("-build must be 1 or more")
	}
	priv, err := update.ParsePrivateKey(pemText)
	check(err)
	pubPEM, err := os.ReadFile("update/public-key.pem")
	check(err)
	pub, err := update.ParsePublicKey(pubPEM)
	check(err)
	if !bytes.Equal(priv.Public().(ed25519.PublicKey), pub) {
		fail("this private key does not match update/public-key.pem; refusing to sign")
	}
	data, err := os.ReadFile(*exe)
	check(err)
	m := update.Manifest{
		App: update.AppID, Build: *build, Version: *version, Created: time.Now().UTC(), Notes: *notes,
		Exe: update.File{Name: update.ExeName, SHA256: update.SHA256Hex(data), Size: int64(len(data))},
	}
	mb, err := json.MarshalIndent(m, "", "  ")
	check(err)
	mb = append(mb, '\n')
	sig := update.Sign(priv, mb)
	if _, err := update.Verify(pub, mb, sig); err != nil {
		fail("self-check failed: " + err.Error())
	}
	check(os.MkdirAll(*out, 0o755))
	check(os.WriteFile(filepath.Join(*out, "manifest.json"), mb, 0o644))
	check(os.WriteFile(filepath.Join(*out, "manifest.sig"), []byte(sig+"\n"), 0o644))
	fmt.Printf("signed build %d (%s, %d bytes)\n", m.Build, m.Exe.SHA256[:12], m.Exe.Size)
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
