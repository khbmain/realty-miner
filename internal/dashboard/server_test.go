package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashboardReadsCurrentFile(t *testing.T) {
	dir := t.TempDir()
	handler := testHandler(t, dir, dir)
	get := func() map[string]any {
		t.Helper()
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", "/api/dashboard", nil))
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if len(get()["items"].([]any)) != 0 {
		t.Fatal("expected empty initial data")
	}
	if err := os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`[{"external_id":"123","title":"Test"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if len(get()["items"].([]any)) != 1 {
		t.Fatal("did not read newly written data")
	}
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`invalid`), 0600)
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/api/dashboard", nil))
	if r.Code != 500 {
		t.Fatal("expected error for corrupt file")
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "Realty Miner") {
		t.Fatal("dashboard HTML missing")
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/data/listings.json", nil))
	if r.Code != 404 {
		t.Fatal("unexpected file exposure")
	}
}

func testHandler(t *testing.T, dir, logs string) http.Handler {
	t.Helper()
	h := Handler(dir, logs)
	t.Cleanup(func() { h.(io.Closer).Close() })
	return h
}
