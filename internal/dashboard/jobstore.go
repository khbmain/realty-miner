package dashboard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"sync"
	"time"
)

type jobStore struct {
	dir       string
	once      sync.Once
	db        *sql.DB
	err       error
	refreshMu sync.Mutex
	checked   time.Time
	stamp     time.Time
}

func (s *jobStore) ready() error {
	s.once.Do(func() {
		q := url.Values{}
		q.Add("_pragma", "busy_timeout(5000)")
		q.Add("_pragma", "journal_mode(WAL)")
		s.db, s.err = sql.Open("sqlite", filepath.Join(s.dir, "collector.sqlite")+"?"+q.Encode())
		if s.err != nil {
			return
		}
		s.db.SetMaxOpenConns(8)
		_, s.err = s.db.Exec(`CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY,url TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','done','retry')),owner TEXT NOT NULL DEFAULT '',lease_until INTEGER NOT NULL DEFAULT 0,retry_after INTEGER NOT NULL DEFAULT 0,started_at INTEGER,finished_at INTEGER,attempts INTEGER NOT NULL DEFAULT 0,last_error TEXT NOT NULL DEFAULT ''); CREATE INDEX IF NOT EXISTS jobs_state ON jobs(state,retry_after,lease_until);CREATE UNIQUE INDEX IF NOT EXISTS jobs_owner ON jobs(owner) WHERE state='processing' AND owner!='';CREATE TABLE IF NOT EXISTS workers(worker TEXT PRIMARY KEY,lease_until INTEGER NOT NULL);`)
		if s.err != nil {
			return
		}
		// Reconcile committed files after a crash between file commit and database commit.
		s.err = s.importRows()
		if s.err != nil {
			return
		}
		tx, e := s.db.Begin()
		if e != nil {
			s.err = e
			return
		}
		defer tx.Rollback()
		for _, d := range detailCache.load(filepath.Join(s.dir, "details")) {
			if d.Complete && (len(d.Phones) > 0 || d.Status == "closed") {
				if _, e = tx.Exec(`UPDATE jobs SET state='done',owner='',lease_until=0,finished_at=? WHERE id=?`, time.Now().UnixMilli(), d.ExternalID); e != nil {
					s.err = e
					return
				}
			}
		}
		files, _ := os.ReadDir(filepath.Join(s.dir, "skipped"))
		for _, f := range files {
			b, e := os.ReadFile(filepath.Join(s.dir, "skipped", f.Name()))
			if e != nil {
				continue
			}
			var skip struct {
				ID     string    `json:"external_id"`
				Retry  time.Time `json:"retry_after"`
				Reason string    `json:"reason"`
			}
			if json.Unmarshal(b, &skip) == nil && time.Now().Before(skip.Retry) {
				if _, e = tx.Exec(`UPDATE jobs SET state='retry',owner='',lease_until=0,retry_after=?,last_error=? WHERE id=? AND state!='done'`, skip.Retry.UnixMilli(), skip.Reason, skip.ID); e != nil {
					s.err = e
					return
				}
			}
		}
		s.err = tx.Commit()
		if info, e := os.Stat(filepath.Join(s.dir, "listings.json")); e == nil {
			s.stamp = info.ModTime()
		}
		s.checked = time.Now()
	})
	return s.err
}
func (s *jobStore) importRows() error {
	var rows []unegui.Listing
	b, e := os.ReadFile(filepath.Join(s.dir, "listings.json"))
	if e == nil {
		if e = json.Unmarshal(b, &rows); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	rows = mergeBrowserListings(s.dir, rows)
	return s.insert(rows)
}
func (s *jobStore) insert(rows []unegui.Listing) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	st, e := tx.Prepare(`INSERT INTO jobs(id,url) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET url=excluded.url WHERE jobs.url!=excluded.url`)
	if e != nil {
		return e
	}
	defer st.Close()
	for _, row := range rows {
		if !detailID.MatchString(row.ExternalID) {
			continue
		}
		if _, e = st.Exec(row.ExternalID, row.URL); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *jobStore) add(rows []unegui.Listing) error {
	if e := s.ready(); e != nil {
		return e
	}
	return s.insert(rows)
}
func (s *jobStore) refresh() {
	if !s.refreshMu.TryLock() {
		return
	}
	defer s.refreshMu.Unlock()
	if time.Since(s.checked) < 30*time.Second {
		return
	}
	s.checked = time.Now()
	info, e := os.Stat(filepath.Join(s.dir, "listings.json"))
	if e == nil && !info.ModTime().Equal(s.stamp) {
		if s.importRows() == nil {
			s.stamp = info.ModTime()
		}
	}
}
func (s *jobStore) claimWorker(id string) (bool, error) {
	if id == "" || len(id) > 100 {
		return false, nil
	}
	if e := s.ready(); e != nil {
		return false, e
	}
	now := time.Now().UnixMilli()
	result, e := s.db.Exec(`INSERT INTO workers(worker,lease_until) SELECT ?,? WHERE EXISTS(SELECT 1 FROM workers WHERE worker=?) OR (SELECT count(*) FROM workers WHERE lease_until>?)<10 ON CONFLICT(worker) DO UPDATE SET lease_until=excluded.lease_until`, id, now+90000, id, now)
	if e != nil {
		return false, e
	}
	n, e := result.RowsAffected()
	if e != nil || n == 0 {
		return false, e
	}
	_, e = s.db.Exec(`UPDATE jobs SET lease_until=? WHERE state='processing' AND owner=? AND lease_until>?`, now+90000, id, now)
	return e == nil, e
}
func (s *jobStore) next(worker string) (map[string]any, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	s.refresh()
	now := time.Now().UnixMilli()
	var id, link string
	e := s.db.QueryRow(`UPDATE jobs SET state='processing',owner=?,lease_until=?,started_at=CASE WHEN state='processing' AND owner=? THEN started_at ELSE ? END,attempts=attempts+CASE WHEN state='processing' AND owner=? THEN 0 ELSE 1 END WHERE id=(SELECT id FROM jobs WHERE (state='processing' AND owner=?) OR state='pending' OR (state='retry' AND retry_after<=?) OR (state='processing' AND lease_until<=?) ORDER BY CASE WHEN state='processing' AND owner=? THEN 0 ELSE 1 END,rowid LIMIT 1) RETURNING id,url`, worker, now+90000, worker, now, worker, worker, now, now, worker).Scan(&id, &link)
	if e != nil && e != sql.ErrNoRows {
		return nil, e
	}
	counts, err := s.counts()
	if err != nil {
		return nil, err
	}
	if e == sql.ErrNoRows {
		counts["done"] = true
		return counts, nil
	}
	counts["external_id"] = id
	counts["url"] = link
	return counts, nil
}
func (s *jobStore) counts() (map[string]any, error) {
	if e := s.ready(); e != nil {
		return nil, e
	}
	var total, done, processing, retry int
	e := s.db.QueryRow(`SELECT count(*),coalesce(sum(state='done'),0),coalesce(sum(state='processing'),0),coalesce(sum(state='retry'),0) FROM jobs`).Scan(&total, &done, &processing, &retry)
	return map[string]any{"total": total, "completed": done, "pending": total - done, "processing": processing, "retry": retry}, e
}
func (s *jobStore) finish(id, worker string, complete bool, retry time.Time, reason string) error {
	if e := s.ready(); e != nil {
		return e
	}
	if !complete && retry.IsZero() {
		return nil
	}
	state := "done"
	if !complete {
		state = "retry"
	}
	result, e := s.db.Exec(`UPDATE jobs SET state=?,owner='',lease_until=0,retry_after=?,finished_at=?,last_error=? WHERE id=? AND (state!='processing' OR owner=? OR ?='') AND (state!='done' OR ?='done')`, state, retry.UnixMilli(), time.Now().UnixMilli(), reason, id, worker, worker, state)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job is assigned to another worker or missing")
	}
	return nil
}

func (s *jobStore) authorize(id, worker string) error {
	if worker == "" {
		return nil
	}
	if e := s.ready(); e != nil {
		return e
	}
	var state, owner string
	e := s.db.QueryRow(`SELECT state,owner FROM jobs WHERE id=?`, id).Scan(&state, &owner)
	if e != nil {
		return e
	}
	if state == "processing" && owner == worker {
		return nil
	}
	if state == "done" {
		return nil
	}
	return fmt.Errorf("job is not assigned to this worker")
}
