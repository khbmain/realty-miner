package dashboard

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Detail struct {
	Status      string            `json:"status,omitempty"`
	Complete    bool              `json:"complete"`
	ExternalID  string            `json:"external_id"`
	URL         string            `json:"url"`
	Phones      []string          `json:"phones"`
	Description string            `json:"description"`
	Attributes  map[string]string `json:"attributes"`
	CollectedAt time.Time         `json:"collected_at"`
}

var detailID = regexp.MustCompile(`^[0-9]{1,15}$`)
var phonePattern = regexp.MustCompile(`^\+?[0-9]{8,15}$`)

func collector(mux *http.ServeMux, dir string) func() {
	os.MkdirAll(dir, 0750)
	tokenPath := filepath.Join(dir, ".collector-token")
	token, err := os.ReadFile(tokenPath)
	if os.IsNotExist(err) {
		random := make([]byte, 32)
		if _, err = rand.Read(random); err == nil {
			token = []byte(hex.EncodeToString(random))
			err = os.WriteFile(tokenPath, token, 0600)
		}
	}
	if err != nil || len(token) < 32 {
		return func() {}
	}
	store := &jobStore{dir: dir}
	claim := func(id string) bool { ok, _ := store.claimWorker(id); return ok }
	queueNext := func(id string) (map[string]any, error) {
		if id == "" {
			return nextDetail(dir)
		}
		return store.next(id)
	}
	mux.HandleFunc("/api/collector/jobs", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		counts, e := store.counts()
		if e != nil {
			http.Error(w, "Cannot read jobs", 500)
			return
		}
		json.NewEncoder(w).Encode(counts)
	})
	mux.HandleFunc("/api/collector/lease", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		if !claim(r.Header.Get("X-Collector-Worker")) {
			http.Error(w, "Another collector tab is active", 409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"active":true,"expires_seconds":90}`))
	})
	mux.HandleFunc("/api/collector/skip", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		if !claim(r.Header.Get("X-Collector-Worker")) {
			http.Error(w, "Another collector tab is active", 409)
			return
		}
		var entry struct {
			ID         string    `json:"external_id"`
			Reason     string    `json:"reason"`
			RetryAfter time.Time `json:"retry_after"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&entry) != nil || !detailID.MatchString(entry.ID) || len(entry.Reason) > 1000 {
			http.Error(w, "Invalid skip", 400)
			return
		}
		if e := store.authorize(entry.ID, r.Header.Get("X-Collector-Worker")); e != nil {
			http.Error(w, "Job is not assigned to this worker", 409)
			return
		}
		entry.RetryAfter = time.Now().Add(30 * time.Minute).UTC()
		folder := filepath.Join(dir, "skipped")
		if os.MkdirAll(folder, 0750) != nil {
			http.Error(w, "Cannot save skip", 500)
			return
		}
		b, _ := json.Marshal(entry)
		if os.WriteFile(filepath.Join(folder, entry.ID+".json"), b, 0600) != nil {
			http.Error(w, "Cannot save skip", 500)
			return
		}
		if e := store.finish(entry.ID, r.Header.Get("X-Collector-Worker"), false, entry.RetryAfter, entry.Reason); e != nil {
			http.Error(w, "Cannot update retry state", 500)
			return
		}
		next, e := queueNext(r.Header.Get("X-Collector-Worker"))
		if e != nil {
			http.Error(w, "Cannot read queue", 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"skipped": true, "next": next})
	})
	mux.HandleFunc("/api/collector/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		var entry struct {
			Mode    string    `json:"mode"`
			Message string    `json:"message"`
			URL     string    `json:"url"`
			At      time.Time `json:"at"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&entry) != nil || (entry.Mode != "details" && entry.Mode != "listings") {
			http.Error(w, "Invalid status", 400)
			return
		}
		entry.At = time.Now().UTC()
		b, _ := json.Marshal(entry)
		f, e := os.OpenFile(filepath.Join(dir, "collector-status.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			http.Error(w, "Cannot save status", 500)
			return
		}
		defer f.Close()
		if _, e = f.Write(append(b, '\n')); e != nil {
			http.Error(w, "Cannot save status", 500)
			return
		}
		w.Write([]byte(`{"saved":true}`))
	})
	mux.HandleFunc("/api/collector/listings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		var payload struct {
			URL  string `json:"url"`
			HTML string `json:"html"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 8*1024*1024)).Decode(&payload) != nil {
			http.Error(w, "Invalid payload", 400)
			return
		}
		u, e := url.Parse(payload.URL)
		if e != nil || u.Scheme != "https" || u.Host != "www.unegui.mn" || !strings.HasPrefix(u.Path, "/l-hdlh/") {
			http.Error(w, "Invalid category", 400)
			return
		}
		rows, e := unegui.Parse(strings.NewReader(payload.HTML))
		if e != nil {
			http.Error(w, "No listing cards found", 400)
			return
		}
		folder := filepath.Join(dir, "browser-listings")
		if os.MkdirAll(folder, 0750) != nil {
			http.Error(w, "Cannot save", 500)
			return
		}
		known := map[string]bool{}
		if b, e := os.ReadFile(filepath.Join(dir, "listings.json")); e == nil {
			var existing []unegui.Listing
			if json.Unmarshal(b, &existing) != nil {
				http.Error(w, "Invalid listings", 500)
				return
			}
			for _, row := range existing {
				known[row.ExternalID] = true
			}
		}
		added := 0
		for _, row := range rows {
			if !known[row.ExternalID] {
				if _, e := os.Stat(filepath.Join(folder, row.ExternalID+".json")); os.IsNotExist(e) {
					added++
				}
			}
		}
		now := time.Now().UTC()
		for _, row := range rows {
			row.TransactionType = unegui.TransactionType(payload.URL)
			row.FirstSeen = now
			row.LastSeen = now
			path := filepath.Join(folder, row.ExternalID+".json")
			if b, e := os.ReadFile(path); e == nil {
				var old unegui.Listing
				if json.Unmarshal(b, &old) == nil {
					row.FirstSeen = old.FirstSeen
					if row.TransactionType == "" {
						row.TransactionType = old.TransactionType
					}
				}
			}
			b, _ := json.Marshal(row)
			f, e := os.CreateTemp(folder, ".listing-*")
			if e != nil {
				http.Error(w, "Cannot save", 500)
				return
			}
			name := f.Name()
			_, e = f.Write(b)
			f.Close()
			if e == nil {
				e = os.Rename(name, path)
			}
			os.Remove(name)
			if e != nil {
				http.Error(w, "Cannot save", 500)
				return
			}
			browserCache.put(folder, row.ExternalID+".json", row)
		}
		if e := store.add(rows); e != nil {
			http.Error(w, "Cannot enqueue listings", 500)
			return
		}
		next := browserNext(payload.HTML, u)
		ack := map[string]any{"saved": true, "saved_url": payload.URL, "count": len(rows), "new_count": added, "duplicate_count": len(rows) - added, "all_duplicates": added == 0, "next": next, "done": next == ""}
		checkpoint, _ := json.Marshal(ack)
		f, e := os.CreateTemp(dir, ".browser-progress-*")
		if e != nil {
			http.Error(w, "Cannot save progress", 500)
			return
		}
		name := f.Name()
		_, e = f.Write(checkpoint)
		f.Close()
		if e == nil {
			e = os.Rename(name, filepath.Join(dir, "browser-progress.json"))
		}
		os.Remove(name)
		if e != nil {
			http.Error(w, "Cannot save progress", 500)
			return
		}
		json.NewEncoder(w).Encode(ack)
	})
	mux.HandleFunc("/api/collector/next", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		if id := r.Header.Get("X-Collector-Worker"); id != "" && !claim(id) {
			http.Error(w, "Ten collector tabs are active", 409)
			return
		}
		next, e := queueNext(r.Header.Get("X-Collector-Worker"))
		if e != nil {
			http.Error(w, "Cannot read queue", 500)
			return
		}
		json.NewEncoder(w).Encode(next)
	})
	mux.HandleFunc("/realty-collector.user.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		script, _ := assets.ReadFile("collector.user.js")
		w.Write([]byte(strings.ReplaceAll(string(script), "__LOCAL_TOKEN__", string(token))))
	})
	mux.HandleFunc("/api/collector/details", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+string(token))) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		var detail Detail
		if worker := r.Header.Get("X-Collector-Worker"); worker != "" && !claim(worker) {
			http.Error(w, "Another collector tab is active", 409)
			return
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 128*1024))
		if decoder.Decode(&detail) != nil || !detailID.MatchString(detail.ExternalID) || !strings.HasPrefix(detail.URL, "https://www.unegui.mn/adv/"+detail.ExternalID+"_") || len(detail.Description) > 50000 || len(detail.Attributes) > 80 || len(detail.Phones) > 10 || (detail.Status != "" && detail.Status != "active" && detail.Status != "closed") {
			http.Error(w, "Invalid detail", 400)
			return
		}
		if e := store.authorize(detail.ExternalID, r.Header.Get("X-Collector-Worker")); e != nil {
			http.Error(w, "Job is not assigned to this worker", 409)
			return
		}
		for _, phone := range detail.Phones {
			if !phonePattern.MatchString(phone) {
				http.Error(w, "Invalid phone", 400)
				return
			}
		}
		detail.CollectedAt = time.Now().UTC()
		folder := filepath.Join(dir, "details")
		if err := os.MkdirAll(folder, 0750); err != nil {
			http.Error(w, "Cannot create details directory", 500)
			return
		}
		path := filepath.Join(folder, detail.ExternalID+".json")
		{
			if old, e := os.ReadFile(path); e == nil {
				var previous Detail
				if json.Unmarshal(old, &previous) == nil {
					if len(detail.Phones) == 0 {
						detail.Phones = previous.Phones
					}
					if detail.Description == "" {
						detail.Description = previous.Description
					}
					if detail.Attributes == nil {
						detail.Attributes = map[string]string{}
					}
					for key, value := range previous.Attributes {
						if _, exists := detail.Attributes[key]; !exists {
							detail.Attributes[key] = value
						}
					}
					detail.Complete = detail.Complete || previous.Complete
				}
			}
		}
		data, _ := json.MarshalIndent(detail, "", "  ")
		temp, e := os.CreateTemp(folder, ".detail-*")
		if e != nil {
			http.Error(w, "Cannot save", 500)
			return
		}
		name := temp.Name()
		defer os.Remove(name)
		if _, e = temp.Write(data); e != nil {
			temp.Close()
			http.Error(w, "Cannot save", 500)
			return
		}
		temp.Close()
		if e = os.Rename(name, path); e != nil {
			http.Error(w, "Cannot save", 500)
			return
		}
		detailCache.put(folder, detail.ExternalID+".json", detail)
		if e := store.finish(detail.ExternalID, r.Header.Get("X-Collector-Worker"), detail.Complete && (len(detail.Phones) > 0 || detail.Status == "closed"), time.Time{}, ""); e != nil {
			http.Error(w, "Cannot update job state", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		next, e := queueNext(r.Header.Get("X-Collector-Worker"))
		if e != nil {
			json.NewEncoder(w).Encode(map[string]any{"saved": true, "saved_id": detail.ExternalID, "queue_error": "Cannot read queue"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"saved": true, "saved_id": detail.ExternalID, "next": next})
	})
	return func() {
		if store.db != nil {
			store.db.Close()
		}
	}
}

// nextDetail is called only after the saved detail file has been committed.
func nextDetail(dir string, exclusions ...map[string]bool) (map[string]any, error) {
	data, e := os.ReadFile(filepath.Join(dir, "listings.json"))
	if e != nil {
		return nil, e
	}
	var rows []struct {
		ID  string `json:"external_id"`
		URL string `json:"url"`
	}
	if json.Unmarshal(data, &rows) != nil {
		return nil, fmt.Errorf("invalid listings")
	}
	merged := mergeBrowserListings(dir, nil)
	known := map[string]bool{}
	for _, r := range rows {
		known[r.ID] = true
	}
	for _, r := range merged {
		if !known[r.ExternalID] {
			rows = append(rows, struct {
				ID  string `json:"external_id"`
				URL string `json:"url"`
			}{r.ExternalID, r.URL})
		}
	}
	done := map[string]bool{}
	for _, d := range detailCache.load(filepath.Join(dir, "details")) {
		if d.Complete && (len(d.Phones) > 0 || d.Status == "closed") {
			done[d.ExternalID] = true
		}
	}
	for _, row := range rows {
		if done[row.ID] || (len(exclusions) > 0 && exclusions[0][row.ID]) {
			continue
		}
		if b, e := os.ReadFile(filepath.Join(dir, "skipped", row.ID+".json")); e == nil {
			var skip struct {
				RetryAfter time.Time `json:"retry_after"`
			}
			if json.Unmarshal(b, &skip) == nil && time.Now().Before(skip.RetryAfter) {
				continue
			}
		}
		if !done[row.ID] && detailID.MatchString(row.ID) && strings.HasPrefix(row.URL, "https://www.unegui.mn/adv/"+row.ID+"_") {
			return map[string]any{"url": row.URL, "external_id": row.ID, "completed": len(done), "total": len(rows)}, nil
		}
	}

	return map[string]any{"done": true, "completed": len(done), "total": len(rows), "pending": len(rows) - len(done)}, nil
}

func mergeBrowserListings(dir string, rows []unegui.Listing) []unegui.Listing {
	index := map[string]int{}
	for i, row := range rows {
		index[row.ExternalID] = i
	}
	for _, row := range browserCache.load(filepath.Join(dir, "browser-listings")) {
		if i, ok := index[row.ExternalID]; ok {
			if row.LastSeen.After(rows[i].LastSeen) {
				row.FirstSeen = rows[i].FirstSeen
				if row.TransactionType == "" {
					row.TransactionType = rows[i].TransactionType
				}
				rows[i] = row
			}
		} else {
			index[row.ExternalID] = len(rows)
			rows = append(rows, row)
		}
	}
	return rows
}

func browserNext(html string, current *url.URL) string {
	page, _ := strconv.Atoi(current.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	doc, e := goquery.NewDocumentFromReader(strings.NewReader(html))
	if e != nil {
		return ""
	}
	next := ""
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u, e := current.Parse(href)
		if e != nil {
			return
		}
		n, _ := strconv.Atoi(u.Query().Get("page"))
		if u.Scheme == current.Scheme && u.Host == current.Host && u.Path == current.Path && n == page+1 {
			next = u.String()
		}
	})
	return next
}
