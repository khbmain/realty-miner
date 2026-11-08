package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"sync"
	"testing"
	"time"
)

func makeJobStore(t *testing.T) *jobStore {
	t.Helper()
	dir := t.TempDir()
	var rows []unegui.Listing
	for i := 1; i <= 20; i++ {
		id := fmt.Sprint(i)
		rows = append(rows, unegui.Listing{ExternalID: id, URL: "https://www.unegui.mn/adv/" + id + "_test/"})
	}
	b, _ := json.Marshal(rows)
	os.WriteFile(filepath.Join(dir, "listings.json"), b, 0600)
	s := &jobStore{dir: dir}
	if e := s.ready(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}
func TestSQLiteConcurrentAssignments(t *testing.T) {
	s := makeJobStore(t)
	var wg sync.WaitGroup
	results := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			worker := fmt.Sprint(i)
			ok, e := s.claimWorker(worker)
			if e != nil || !ok {
				t.Errorf("claim: %v %v", ok, e)
				return
			}
			n, e := s.next(worker)
			if e != nil {
				t.Error(e)
				return
			}
			id := n["external_id"].(string)
			again, e := s.next(worker)
			if e != nil || again["external_id"] != id {
				t.Errorf("assignment changed: %v %v", again, e)
			}
			results <- id
		}(i)
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for id := range results {
		if seen[id] {
			t.Fatal("duplicate job", id)
		}
		seen[id] = true
	}
	if len(seen) != 10 {
		t.Fatal("not ten workers")
	}
	if ok, e := s.claimWorker("eleventh"); ok || e != nil {
		t.Fatalf("eleventh worker: %v %v", ok, e)
	}
}
func TestSQLiteRestartAndExpiredLease(t *testing.T) {
	s := makeJobStore(t)
	n, e := s.next("a")
	if e != nil {
		t.Fatal(e)
	}
	id := n["external_id"].(string)
	s.db.Close()
	restored := &jobStore{dir: s.dir}
	if e = restored.ready(); e != nil {
		t.Fatal(e)
	}
	defer restored.db.Close()
	n, e = restored.next("a")
	if e != nil || n["external_id"] != id {
		t.Fatalf("restart lost processing: %v %v", n, e)
	}
	restored.db.Exec(`UPDATE jobs SET lease_until=0 WHERE id=?`, id)
	n, e = restored.next("b")
	if e != nil || n["external_id"] != id {
		t.Fatalf("expired lease not reclaimed: %v %v", n, e)
	}
	if restored.authorize(id, "a") == nil {
		t.Fatal("stale worker authorized")
	}
	if e = restored.finish(id, "b", true, time.Time{}, ""); e != nil {
		t.Fatal(e)
	}
	n, e = restored.next("b")
	if e != nil || n["external_id"] == id || n["completed"] != 1 {
		t.Fatalf("done repeated: %v %v", n, e)
	}
}
func TestSQLiteRetryState(t *testing.T) {
	s := makeJobStore(t)
	n, _ := s.next("a")
	id := n["external_id"].(string)
	if e := s.finish(id, "a", false, time.Now().Add(time.Hour), "timeout"); e != nil {
		t.Fatal(e)
	}
	next, _ := s.next("a")
	if next["external_id"] == id {
		t.Fatal("retry selected early")
	}
	s.db.Exec(`UPDATE jobs SET retry_after=0 WHERE id=?`, id)
	s.finish(next["external_id"].(string), "a", true, time.Time{}, "")
	next, _ = s.next("a")
	if next["external_id"] != id {
		t.Fatal("retry not restored", next)
	}
}
