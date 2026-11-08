package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrowserListingImport(t *testing.T) {
	dir := t.TempDir()
	h := testHandler(t, dir, t.TempDir())
	token, _ := os.ReadFile(filepath.Join(dir, ".collector-token"))
	html := `<article itemscope itemtype="https://schema.org/Product"><a itemprop="url" href="/adv/123_test/"></a><meta itemprop="name" content="Test listing"><meta itemprop="image" content="https://cdn1.unegui.mn/thumb.jpg"></article>`
	b, _ := json.Marshal(map[string]string{"url": "https://www.unegui.mn/l-hdlh/?page=99", "html": html})
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodPost, "/api/collector/listings", strings.NewReader(string(b)))
		r.Header.Set("Authorization", "Bearer "+string(token))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var ack struct {
			NewCount      int  `json:"new_count"`
			AllDuplicates bool `json:"all_duplicates"`
		}
		if json.Unmarshal(w.Body.Bytes(), &ack) != nil {
			t.Fatal("invalid ack")
		}
		if ack.NewCount != 1-i || ack.AllDuplicates != (i == 1) {
			t.Fatalf("bad duplicate detection: %+v", ack)
		}
		if w.Code != 200 {
			t.Fatalf("import: %d %s", w.Code, w.Body.String())
		}
	}
	rows := mergeBrowserListings(dir, nil)
	if len(rows) != 1 || rows[0].ImageURL != "https://cdn1.unegui.mn/thumb.jpg" {
		t.Fatalf("bad rows: %+v", rows)
	}
}

func TestBrowserNext(t *testing.T) {
	u, _ := url.Parse("https://www.unegui.mn/l-hdlh/?page=99")
	html := `<a href="?page=98">Өмнөх</a><a href="https://evil.example/l-hdlh/?page=100">bad</a><a href="?page=100">Дараагийнх</a>`
	if got := browserNext(html, u); got != "https://www.unegui.mn/l-hdlh/?page=100" {
		t.Fatal(got)
	}
	if browserNext(`<a href="?page=99">99</a>`, u) != "" {
		t.Fatal("must stop at end")
	}
}

func TestSkippedListingRetriesLater(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`[{"external_id":"1","url":"https://www.unegui.mn/adv/1_one/"},{"external_id":"2","url":"https://www.unegui.mn/adv/2_two/"}]`), 0600)
	os.MkdirAll(filepath.Join(dir, "skipped"), 0700)
	for _, future := range []bool{true, false} {
		at := time.Now().Add(-time.Minute)
		want := "1"
		if future {
			at = time.Now().Add(time.Minute)
			want = "2"
		}
		b, _ := json.Marshal(map[string]any{"retry_after": at})
		os.WriteFile(filepath.Join(dir, "skipped", "1.json"), b, 0600)
		next, e := nextDetail(dir)
		if e != nil || next["external_id"] != want {
			t.Fatalf("next=%v err=%v", next, e)
		}
	}
}

func TestFiveWorkersDifferentJobs(t *testing.T) {
	dir := t.TempDir()
	rows := `[{"external_id":"1","url":"https://www.unegui.mn/adv/1_one/"},{"external_id":"2","url":"https://www.unegui.mn/adv/2_two/"}]`
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(rows), 0600)
	h := testHandler(t, dir, t.TempDir())
	token, _ := os.ReadFile(filepath.Join(dir, ".collector-token"))
	ids := map[string]bool{}
	for _, worker := range []string{"a", "b"} {
		r := httptest.NewRequest("GET", "/api/collector/next", nil)
		r.Header.Set("Authorization", "Bearer "+string(token))
		r.Header.Set("X-Collector-Worker", worker)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		id, _ := result["external_id"].(string)
		if w.Code != 200 || id == "" || ids[id] {
			t.Fatalf("duplicate job: %s", w.Body.String())
		}
		ids[id] = true
	}
}
