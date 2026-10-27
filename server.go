package main

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(v)
}

// localOnly rejects requests that did not come from our own UI, so other
// web pages open in a browser can't drive the local server.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(r.Host, "127.0.0.1:") && !strings.HasPrefix(r.Host, "localhost:") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))

	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		st := map[string]any{
			"appVersion": appVersion, "dataDir": a.dir, "simcPath": a.simcPath(),
			"settings": a.getSettings(), "threads": a.threads(), "fightStyles": fightStyles,
		}
		if in, ok := a.managedInstall(); ok {
			st["install"] = in
		}
		if v, err := a.simcVersion(); err == nil {
			st["simcVersion"] = v
			st["engine"] = engineInfo(v)
		} else if a.simcPath() != "" {
			st["simcError"] = err.Error()
		}
		if j := a.jobs.running("install"); j != nil {
			st["installJob"] = j.ID
		}
		if j := a.jobs.running("sim"); j != nil {
			st["simJob"] = j.ID
		}
		writeJSON(w, st)
	})

	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var s Settings
		if err := readJSON(r, &s); err != nil {
			writeErr(w, 400, err)
			return
		}
		if s.SimcPath != "" {
			if st, err := os.Stat(s.SimcPath); err == nil && st.IsDir() {
				s.SimcPath = filepath.Join(s.SimcPath, simcExeName())
			}
			if _, err := os.Stat(s.SimcPath); err != nil {
				writeErr(w, 400, errors.New("no simc executable at "+s.SimcPath))
				return
			}
		}
		if s.NightlyURL == "" {
			s.NightlyURL = defaultNightlyURL
		}
		if err := a.saveSettings(s); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, s)
	})

	mux.HandleFunc("POST /api/simc/install", func(w http.ResponseWriter, r *http.Request) {
		if j := a.jobs.running("install"); j != nil {
			writeJSON(w, map[string]string{"job": j.ID})
			return
		}
		j := a.jobs.start("install", "Install SimulationCraft", func(j *job) (string, error) {
			return "", a.installLatest(j.ctx, j)
		})
		writeJSON(w, map[string]string{"job": j.ID})
	})

	// Install from an archive the user picked (works fully offline).
	mux.HandleFunc("POST /api/simc/upload", func(w http.ResponseWriter, r *http.Request) {
		name := sanitizeName(r.URL.Query().Get("name"))
		tmp := a.path("tmp", "upload-"+name)
		f, err := os.Create(tmp)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		_, err = io.Copy(f, io.LimitReader(r.Body, 2<<30))
		f.Close()
		if err != nil {
			os.Remove(tmp)
			writeErr(w, 400, err)
			return
		}
		j := a.jobs.start("install", "Install SimulationCraft", func(j *job) (string, error) {
			defer os.Remove(tmp)
			return "", a.installFromArchive(j.ctx, j, tmp, name, "uploaded file")
		})
		writeJSON(w, map[string]string{"job": j.ID})
	})

	mux.HandleFunc("GET /api/classes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, classes)
	})

	mux.HandleFunc("GET /api/apl/default", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		apl, err := a.defaultAPL(q.Get("class"), q.Get("spec"), q.Get("refresh") == "1")
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"content": apl, "simcVersion": a.simcVersionKey()})
	})

	mux.HandleFunc("GET /api/apl/custom", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.listCustomAPLs())
	})
	mux.HandleFunc("GET /api/apl/custom/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, err := a.getCustomAPL(r.PathValue("id"))
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		writeJSON(w, c)
	})
	mux.HandleFunc("POST /api/apl/custom", func(w http.ResponseWriter, r *http.Request) {
		var c CustomAPL
		if err := readJSON(r, &c); err != nil {
			writeErr(w, 400, err)
			return
		}
		c, err := a.saveCustomAPL(c)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, c)
	})
	mux.HandleFunc("DELETE /api/apl/custom/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := a.deleteCustomAPL(r.PathValue("id")); err != nil {
			writeErr(w, 404, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.sampleProfiles())
	})
	mux.HandleFunc("GET /api/profile", func(w http.ResponseWriter, r *http.Request) {
		txt, err := a.readSampleProfile(r.URL.Query().Get("path"))
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		writeJSON(w, map[string]string{"content": txt})
	})
	mux.HandleFunc("POST /api/parse", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Profile string `json:"profile"`
		}
		if err := readJSON(r, &body); err != nil {
			writeErr(w, 400, err)
			return
		}
		info := parseProfile(body.Profile)
		resp := map[string]any{}
		b, _ := json.Marshal(info)
		_ = json.Unmarshal(b, &resp)
		if v, err := a.simcVersion(); err == nil && info.Class != "" {
			resp["engineWarning"] = compatProblem(engineInfo(v), info, body.Profile)
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("POST /api/preview", func(w http.ResponseWriter, r *http.Request) {
		var req SimRequest
		if err := readJSON(r, &req); err != nil {
			writeErr(w, 400, err)
			return
		}
		input, combos, err := a.buildInput(req)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]any{"input": input, "combos": len(combos)})
	})

	mux.HandleFunc("POST /api/sim", func(w http.ResponseWriter, r *http.Request) {
		var req SimRequest
		if err := readJSON(r, &req); err != nil {
			writeErr(w, 400, err)
			return
		}
		j, err := a.startSim(req)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"job": j.ID})
	})

	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j := a.jobs.get(r.PathValue("id"))
		if j == nil {
			writeErr(w, 404, errors.New("job not found"))
			return
		}
		since, _ := strconv.Atoi(r.URL.Query().Get("since"))
		writeJSON(w, j.snapshot(since))
	})
	mux.HandleFunc("POST /api/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if j := a.jobs.get(r.PathValue("id")); j != nil {
			j.cancel()
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/results", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, a.history())
	})
	mux.HandleFunc("GET /api/results/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeName(r.PathValue("id"))
		b, err := os.ReadFile(a.path("results", id, "summary.json"))
		if err != nil {
			writeErr(w, 404, errors.New("result not found"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	mux.HandleFunc("GET /api/results/{id}/input", func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeName(r.PathValue("id"))
		in, _ := os.ReadFile(a.path("results", id, "input.simc"))
		req, _ := os.ReadFile(a.path("results", id, "request.json"))
		writeJSON(w, map[string]any{"input": string(in), "request": json.RawMessage(orNull(req))})
	})
	mux.HandleFunc("DELETE /api/results/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeName(r.PathValue("id"))
		if err := os.RemoveAll(a.path("results", id)); err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Full simc HTML report and raw files.
	mux.HandleFunc("GET /reports/{id}/{file}", func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeName(r.PathValue("id"))
		file := r.PathValue("file")
		switch file {
		case "report.html", "result.json", "input.simc", "log.txt":
		default:
			http.NotFound(w, r)
			return
		}
		if file == "input.simc" || file == "log.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		http.ServeFile(w, r, a.path("results", id, file))
	})

	mux.HandleFunc("POST /api/open", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			What string `json:"what"`
			ID   string `json:"id"`
		}
		_ = readJSON(r, &body)
		switch body.What {
		case "data":
			openPath(a.dir)
		case "report":
			openPath(a.path("results", sanitizeName(body.ID), "report.html"))
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		go func() { time.Sleep(200 * time.Millisecond); a.requestQuit() }()
	})
	return localOnly(mux)
}

func orNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}
