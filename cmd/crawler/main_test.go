package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"regexp"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCyclePersistsAndUpdates(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/list.html")
	if err != nil {
		t.Fatal(err)
	}
	c := &crawler{client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := string(fixture)
		if r.URL.Path == "/robots.txt" {
			body = "User-agent: *\nDisallow: /api\n"
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	dir := t.TempDir()
	if err := cycle(context.Background(), c, origin+"/l-hdlh/", dir, 1); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "listings.json")
	before, _ := os.ReadFile(path)
	var initial []unegui.Listing
	if err := json.Unmarshal(before, &initial); err != nil {
		t.Fatal(err)
	}
	if err := cycle(context.Background(), c, origin+"/l-hdlh/", dir, 1); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	var updated []unegui.Listing
	json.Unmarshal(after, &updated)
	if len(initial) != len(updated) || !initial[0].FirstSeen.Equal(updated[0].FirstSeen) || updated[0].LastSeen.Before(initial[0].LastSeen) {
		t.Fatal("upsert lost identity/timestamps")
	}
	if _, err := c.get(context.Background(), origin+"/api/listings"); err == nil {
		t.Fatal("robots disallow ignored")
	}
}

func TestForbiddenNotRetried(t *testing.T) {
	calls := 0
	c := &crawler{client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("Forbidden")), Header: make(http.Header)}, nil
	})}}
	if _, err := c.get(context.Background(), origin+"/l-hdlh/"); err == nil || calls != 1 {
		t.Fatal("403 must fail once")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pause(ctx, time.Hour); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestFullCrawlResumesAfterFailure(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/list.html")
	if err != nil {
		t.Fatal(err)
	}
	fixture = regexp.MustCompile(`page=[0-9]+`).ReplaceAll(fixture, []byte("page=2"))
	fixture = []byte(strings.ReplaceAll(string(fixture), "/l-hdlh/?page=", "/l-hdlh/l-hdlh-zarna/?page="))
	failed := false
	pageOneCalls := 0
	c := &crawler{full: true, client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status := 200
		body := string(fixture)
		if r.URL.Path == "/robots.txt" {
			body = "User-agent: *\nDisallow: /api\n"
		} else if r.URL.Query().Get("page") == "2" && !failed {
			failed = true
			status = 403
		} else if r.URL.RawQuery == "" {
			pageOneCalls++
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	dir := t.TempDir()
	seed := origin + "/l-hdlh/l-hdlh-zarna/"
	if err := cycle(context.Background(), c, seed, dir, 1); err == nil {
		t.Fatal("expected interrupted crawl")
	}
	var state progress
	data, _ := os.ReadFile(checkpointPath(dir, seed))
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.NextPage != 2 || state.Completed || state.TotalPages != 2 {
		t.Fatalf("bad checkpoint: %+v", state)
	}
	if err := cycle(context.Background(), c, seed, dir, 1); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(checkpointPath(dir, seed))
	json.Unmarshal(data, &state)
	if !state.Completed || pageOneCalls != 1 {
		t.Fatalf("did not resume: %+v first-page requests=%d", state, pageOneCalls)
	}
}
