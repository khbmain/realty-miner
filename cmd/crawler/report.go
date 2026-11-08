package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pageReport struct {
	NewIDs   []string `json:"new_ids,omitempty"`
	Category string   `json:"category"`
	Page     int      `json:"page"`
	Observed int      `json:"observed"`
	New      int      `json:"new_listings"`
	Existing int      `json:"existing"`
}
type runReport struct {
	PageDetails []pageReport `json:"page_details,omitempty"`
	ID          string       `json:"id"`
	Started     time.Time    `json:"started_at"`
	Updated     time.Time    `json:"updated_at"`
	Finished    *time.Time   `json:"finished_at,omitempty"`
	Status      string       `json:"status"`
	Pages       int          `json:"pages"`
	Limit       int          `json:"page_limit_per_category"`
	Observed    int          `json:"observed"`
	New         int          `json:"new_listings"`
	Existing    int          `json:"existing"`
	Category    string       `json:"category"`
	Page        int          `json:"page"`
	Errors      []string     `json:"errors"`
	known       map[string]bool
	path        string
}

func beginReport(out string, limit int) (*runReport, error) {
	dir := filepath.Join(out, "runs")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r := &runReport{ID: now.Format("20060102T150405.000000000"), Started: now, Status: "running", Limit: limit, Errors: []string{}, known: map[string]bool{}}
	r.path = filepath.Join(dir, r.ID+".json")
	if b, e := os.ReadFile(filepath.Join(out, "listings.json")); e == nil {
		var rows []struct {
			ID string `json:"external_id"`
		}
		if e = json.Unmarshal(b, &rows); e != nil {
			return nil, e
		}
		for _, row := range rows {
			r.known[row.ID] = true
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	files, e := os.ReadDir(filepath.Join(out, "browser-listings"))
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			r.known[strings.TrimSuffix(f.Name(), ".json")] = true
		}
	}
	return r, r.write()
}
func (r *runReport) write() error { r.Updated = time.Now().UTC(); return saveJSON(r.path, r) }
func (r *runReport) finish(status string) error {
	now := time.Now().UTC()
	r.Finished = &now
	r.Status = status
	return r.write()
}
