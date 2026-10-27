// Creates an Ed25519 key pair for signing OfflineSim updates.
//
//	go run ./tools/keygen <private-key-output-file>
//
// The PRIVATE key stays with you (never commit it; store it as the
// SIGNING_PRIVATE_KEY repository secret). The PUBLIC key is written to
// update/public-key.pem and baked into the app, so changing it means every
// installed copy has to be replaced by hand once.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/keygen <private-key-file>")
		os.Exit(1)
	}
	out := os.Args[1]
	if _, err := os.Stat(out); err == nil {
		fmt.Fprintln(os.Stderr, "refusing to overwrite "+out)
		os.Exit(1)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	check(err)
	pk8, err := x509.MarshalPKCS8PrivateKey(priv)
	check(err)
	pkix, err := x509.MarshalPKIXPublicKey(pub)
	check(err)
	check(os.WriteFile(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk8}), 0o600))
	check(os.WriteFile("update/public-key.pem", pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix}), 0o644))
	fmt.Println("private key -> " + out + "\npublic key  -> update/public-key.pem")
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
