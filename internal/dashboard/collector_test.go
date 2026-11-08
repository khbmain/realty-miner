package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectorAuthAndMerge(t *testing.T) {
	dir := t.TempDir()
	h := testHandler(t, dir, dir)
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`[{"external_id":"123","title":"Apartment"}]`), 0600)
	body := `{"external_id":"123","url":"https://www.unegui.mn/adv/123_test/","phones":["+97699000000"],"attributes":{"Талбай":"42 м²"}}`
	request := func(auth string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/collector/details", strings.NewReader(body))
		r.Header.Set("Authorization", auth)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if request("").Code != 401 {
		t.Fatal("missing token accepted")
	}
	token, err := os.ReadFile(filepath.Join(dir, ".collector-token"))
	if err != nil {
		t.Fatal(err)
	}
	if w := request("Bearer " + string(token)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/dashboard", nil))
	var result struct {
		Items []struct {
			Phones []string `json:"phones"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || len(result.Items[0].Phones) != 1 {
		t.Fatal("details not merged")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/realty-collector.user.js", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "__LOCAL_TOKEN__") {
		t.Fatal("collector not configured")
	}
}

func TestQueueSkipsCompletedDetails(t *testing.T) {
	dir := t.TempDir()
	h := testHandler(t, dir, dir)
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`[{"external_id":"123","url":"https://www.unegui.mn/adv/123_test/"},{"external_id":"456","url":"https://www.unegui.mn/adv/456_test/"}]`), 0600)
	os.MkdirAll(filepath.Join(dir, "details"), 0750)
	token, _ := os.ReadFile(filepath.Join(dir, ".collector-token"))
	next := func() map[string]any {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/collector/next", nil)
		r.Header.Set("Authorization", "Bearer "+string(token))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	if next()["external_id"] != "123" {
		t.Fatal("wrong first item")
	}
	os.WriteFile(filepath.Join(dir, "details", "123.json"), []byte(`{"external_id":"123","complete":true,"phones":["+97699000000"]}`), 0600)
	if next()["external_id"] != "456" {
		t.Fatal("completed item was repeated")
	}
	os.WriteFile(filepath.Join(dir, "details", "456.json"), []byte(`{"external_id":"456","complete":true,"phones":["+97699000000"]}`), 0600)
	if next()["done"] != true {
		t.Fatal("queue did not finish")
	}
}

func TestSaveAcknowledgesAndAdvancesChain(t *testing.T) {
	dir := t.TempDir()
	h := testHandler(t, dir, dir)
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`[{"external_id":"123","url":"https://www.unegui.mn/adv/123_test/"},{"external_id":"456","url":"https://www.unegui.mn/adv/456_test/"}]`), 0600)
	token, _ := os.ReadFile(filepath.Join(dir, ".collector-token"))
	save := func(id string) map[string]any {
		t.Helper()
		body := `{"external_id":"` + id + `","url":"https://www.unegui.mn/adv/` + id + `_test/","phones":["+97699000000"],"complete":true}`
		r := httptest.NewRequest("POST", "/api/collector/details", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+string(token))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	ack := save("123")
	if ack["saved"] != true || ack["saved_id"] != "123" || ack["next"].(map[string]any)["external_id"] != "456" {
		t.Fatalf("bad chain response: %v", ack)
	}
	ack = save("123")
	if ack["next"].(map[string]any)["external_id"] != "456" {
		t.Fatal("duplicate delivery repeated completed ID")
	}
	ack = save("456")
	if ack["next"].(map[string]any)["done"] != true {
		t.Fatal("chain did not terminate")
	}
}

func TestFiveAutoCollectors(t *testing.T) {
	dir := t.TempDir()
	h := testHandler(t, dir, dir)
	token, _ := os.ReadFile(filepath.Join(dir, ".collector-token"))
	claim := func(worker, auth string) int {
		r := httptest.NewRequest("GET", "/api/collector/lease", nil)
		r.Header.Set("Authorization", auth)
		r.Header.Set("X-Collector-Worker", worker)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if claim("tab-a", "") != 401 {
		t.Fatal("unauthenticated lease accepted")
	}
	if claim("tab-a", "Bearer "+string(token)) != 200 {
		t.Fatal("first tab failed")
	}
	for _, id := range []string{"tab-b", "tab-c", "tab-d", "tab-e", "tab-f", "tab-g", "tab-h", "tab-i", "tab-j"} {
		if claim(id, "Bearer "+string(token)) != 200 {
			t.Fatal("worker rejected")
		}
	}
	if claim("tab-k", "Bearer "+string(token)) != 409 {
		t.Fatal("eleventh worker allowed")
	}
	if claim("tab-a", "Bearer "+string(token)) != 200 {
		t.Fatal("same-tab renewal failed")
	}
}

func TestClosedListingAdvancesWithoutPhone(t *testing.T) {
	dir := t.TempDir()
	h := testHandler(t, dir, dir)
	os.WriteFile(filepath.Join(dir, "listings.json"), []byte(`[{"external_id":"123","url":"https://www.unegui.mn/adv/123_test/"},{"external_id":"456","url":"https://www.unegui.mn/adv/456_test/"}]`), 0600)
	token, _ := os.ReadFile(filepath.Join(dir, ".collector-token"))
	r := httptest.NewRequest("POST", "/api/collector/details", strings.NewReader(`{"external_id":"123","url":"https://www.unegui.mn/adv/123_test/","status":"closed","complete":true,"phones":[]}`))
	r.Header.Set("Authorization", "Bearer "+string(token))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var ack map[string]any
	json.Unmarshal(w.Body.Bytes(), &ack)
	if ack["next"].(map[string]any)["external_id"] != "456" {
		t.Fatal("closed listing blocked queue")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "details", "123.json"))
	var d Detail
	json.Unmarshal(data, &d)
	if d.Status != "closed" || !d.Complete || len(d.Phones) != 0 {
		t.Fatal("invalid closed record")
	}
}
