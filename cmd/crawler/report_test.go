package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunReportKnownAndFinish(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "listings.json"), []byte(`[{"external_id":"1"}]`), 0600)
	os.Mkdir(filepath.Join(d, "browser-listings"), 0750)
	os.WriteFile(filepath.Join(d, "browser-listings", "2.json"), []byte(`{}`), 0600)
	r, e := beginReport(d, 99)
	if e != nil {
		t.Fatal(e)
	}
	if !r.known["1"] || !r.known["2"] || r.known["3"] {
		t.Fatal("wrong baseline")
	}
	r.Errors = append(r.Errors, "HTTP 403")
	if e = r.finish("failed"); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(r.path)
	var got runReport
	if e = json.Unmarshal(b, &got); e != nil {
		t.Fatal(e)
	}
	if got.Status != "failed" || got.Finished == nil || got.Limit != 99 || len(got.Errors) != 1 {
		t.Fatal("report not persisted")
	}
}
