package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"offlinesim/internal/update"
)

type relFixture struct {
	srv     *httptest.Server
	files   map[string][]byte
	restore func()
	priv    ed25519.PrivateKey
}

// newRelease serves a signed release over TLS and points the updater at it.
func newRelease(t *testing.T, build int, exe []byte, tamper func(map[string][]byte)) *relFixture {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pkix, _ := x509.MarshalPKIXPublicKey(pub)
	m := update.Manifest{App: update.AppID, Build: build, Version: "1.0." + itoa(build), Created: time.Now(),
		Exe: update.File{Name: update.ExeName, SHA256: update.SHA256Hex(exe), Size: int64(len(exe))}}
	mb, _ := json.Marshal(m)
	files := map[string][]byte{"manifest.json": mb, "manifest.sig": []byte(update.Sign(priv, mb)), update.ExeName: exe}
	if tamper != nil {
		tamper(files)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	oldKey, oldBase, oldHosts := updatePublicKey, updateBase, updateHosts
	updatePublicKey = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkix})
	updateBase = srv.URL + "/dl/"
	u, _ := url.Parse(srv.URL)
	updateHosts = []string{u.Hostname()}
	return &relFixture{srv: srv, files: files, priv: priv, restore: func() {
		srv.Close()
		updatePublicKey, updateBase, updateHosts = oldKey, oldBase, oldHosts
	}}
}

func itoa(n int) string { return strconv.Itoa(n) }

func testUpdater(t *testing.T, f *relFixture) *selfUpdater {
	u := newSelfUpdater(t.TempDir())
	u.client.Transport = f.srv.Client().Transport
	return u
}

func TestUpdateDownloadsAndVerifies(t *testing.T) {
	appBuild = "3"
	defer func() { appBuild = "0" }()
	f := newRelease(t, 4, []byte("new exe bytes"), nil)
	defer f.restore()
	u := testUpdater(t, f)
	if err := u.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := u.get(); s.State != "ready" || s.Latest != 4 {
		t.Fatalf("status %+v", s)
	}
	pub, _ := update.ParsePublicKey(updatePublicKey)
	if _, err := u.staged(pub); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRejects(t *testing.T) {
	appBuild = "3"
	defer func() { appBuild = "0" }()
	cases := map[string]struct {
		build  int
		tamper func(map[string][]byte)
		state  string
		errHas string
	}{
		"older build":   {3, nil, "current", ""},
		"bad signature": {4, func(m map[string][]byte) { m["manifest.json"] = append(m["manifest.json"], ' ') }, "error", "signature"},
		"swapped exe":   {4, func(m map[string][]byte) { m[update.ExeName] = []byte("evil exe byte") }, "error", "hash"},
		"no release":    {4, func(m map[string][]byte) { delete(m, "manifest.json") }, "current", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRelease(t, c.build, []byte("new exe bytes"), c.tamper)
			defer f.restore()
			u := testUpdater(t, f)
			_ = u.check(context.Background())
			s := u.get()
			if s.State != c.state || !strings.Contains(s.Error, c.errHas) {
				t.Fatalf("status %+v", s)
			}
		})
	}
}

func TestUpdateHostAllowlist(t *testing.T) {
	for _, bad := range []string{"http://github.com/x", "https://evil.example/x", "https://github.com.evil.example/x"} {
		u, _ := url.Parse(bad)
		if allowedUpdateURL(u) == nil {
			t.Errorf("%s allowed", bad)
		}
	}
	u, _ := url.Parse("https://objects.githubusercontent.com/x")
	if err := allowedUpdateURL(u); err != nil {
		t.Error(err)
	}
}

func TestEmbeddedPublicKey(t *testing.T) {
	if _, err := update.ParsePublicKey(updatePublicKey); err != nil {
		t.Fatal(err)
	}
}
