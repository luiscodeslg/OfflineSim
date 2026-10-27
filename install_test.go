package main

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLatestNightly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><table>
<tr><td><a href="simc-1205-01-winarm64-ffff111.7z">simc-1205-01-winarm64-ffff111.7z</a></td></tr>
<tr><td><a href="simc-1205-01-win64-abc1234.7z">simc-1205-01-win64-abc1234.7z</a></td><td>2026-10-27 03:10</td></tr>
<tr><td><a href="simc-1205-01-win64-0000999.7z">simc-1205-01-win64-0000999.7z</a></td><td>2026-10-26 03:10</td></tr>
</table></body></html>`))
	}))
	defer srv.Close()
	name, url, err := latestNightly(context.Background(), srv.URL+"/nightly/?C=M;O=D")
	if err != nil {
		t.Fatal(err)
	}
	if name != "simc-1205-01-win64-abc1234.7z" || url != srv.URL+"/nightly/simc-1205-01-win64-abc1234.7z" {
		t.Fatalf("got %s %s", name, url)
	}
}

func TestInstallFromZip(t *testing.T) {
	dir := t.TempDir()
	a, err := newApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(dir, "simc.zip")
	f, _ := os.Create(arc)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("simc-1205-01-win64/" + simcExeName())
	w.Write([]byte("#!/bin/sh\necho 'SimulationCraft 1205-01 for World of Warcraft 12.0.5 Live'\n"))
	w, _ = zw.Create("simc-1205-01-win64/profiles/MID1/MID1_Mage_Fire.simc")
	w.Write([]byte("mage=\"MID1_Mage_Fire\"\nspec=fire\n"))
	zw.Close()
	f.Close()

	j := &job{State: jobRunning}
	j.ctx, j.cancel = contextWithTimeout(10e9)
	defer j.cancel()
	if err := a.installFromArchive(j.ctx, j, arc, "simc-1205-01-win64.zip", "test"); err != nil {
		t.Fatal(err)
	}
	if a.simcPath() == "" {
		t.Fatal("no simc after install")
	}
	ps := a.sampleProfiles()
	if len(ps) != 1 || ps[0].Tier != "MID1" {
		t.Fatalf("profiles: %+v", ps)
	}
	if _, err := a.readSampleProfile("../../settings.json"); err == nil {
		t.Fatal("path traversal not blocked")
	}
}

func TestSummarize(t *testing.T) {
	raw := []byte(`{"version":"1205-01","sim":{"options":{"fight_style":"Patchwerk","max_time":300,"iterations":5000,"desired_targets":1},
	"players":[{"name":"T","specialization":"Fire Mage","race":"gnome","level":90,"role":"spell","talents":{"x":1},
	"collected_data":{"fight_length":{"mean":300},"dps":{"mean":1000,"min":900,"max":1100,"std_dev":30,"mean_std_dev":0.5}},
	"stats":[{"name":"fireball","spell_name":"Fireball","id":133,"school":"fire","type":"damage","num_executes":{"mean":100},"compound_amount":180000},
	         {"name":"heal","type":"heal","compound_amount":5000},
	         {"name":"ignite","school":"fire","type":"damage","num_executes":{"mean":0},"compound_amount":120000}],
	"scale_factors":{"Int":1.5,"Crit":1.1}}],
	"statistics":{"elapsed_cpu_seconds":2},
	"profilesets":{"results":[{"name":"Combo 1","mean":1010,"stddev":30,"iterations":5000},{"name":"Combo 2","mean":1020,"stddev":30,"mean_error":1}]}}}`)
	s, err := summarize(raw)
	if err != nil {
		t.Fatal(err)
	}
	a := s.Actors[0]
	if a.DPS.Mean != 1000 || len(a.Abilities) != 2 || a.Abilities[0].Name != "Fireball" || a.Abilities[1].Name != "Ignite" {
		t.Fatalf("actor: %+v", a)
	}
	if a.Abilities[0].Share != 0.6 {
		t.Fatalf("share %v", a.Abilities[0].Share)
	}
	if len(s.Profilesets) != 2 || s.Profilesets[0].Name != "Combo 2" || s.Profilesets[1].Error == 0 {
		t.Fatalf("profilesets: %+v", s.Profilesets)
	}
}
