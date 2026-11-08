package dashboard

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"sort"
	"strings"
	"time"
)

//go:embed index.html collector.user.js analysis.html analysis.js search.js vendor
var assets embed.FS

func Handler(dataDir, logDir string) http.Handler {
	mux := http.NewServeMux()
	closeCollector := collector(mux, dataDir)
	mux.HandleFunc("/api/runs", func(w http.ResponseWriter, r *http.Request) {
		files, err := filepath.Glob(filepath.Join(dataDir, "runs", "*.json"))
		if err != nil {
			http.Error(w, "Cannot read run history", 500)
			return
		}
		sort.Sort(sort.Reverse(sort.StringSlice(files)))
		if len(files) > 100 {
			files = files[:100]
		}
		runs := []json.RawMessage{}
		for _, file := range files {
			b, e := os.ReadFile(file)
			if e == nil && json.Valid(b) {
				runs = append(runs, json.RawMessage(b))
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{"runs": runs})
	})
	for route, file := range map[string]string{"/analised-data": "analysis.html", "/analysis.js": "analysis.js", "/search.js": "search.js", "/vendor/leaflet.js": "vendor/leaflet.js", "/vendor/leaflet.css": "vendor/leaflet.css", "/vendor/districts.js": "vendor/districts.js"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != route {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if strings.HasSuffix(file, ".css") {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
			}
			if strings.HasSuffix(file, ".js") {
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			}
			w.Header().Set("Cache-Control", "no-cache")
			body, _ := assets.ReadFile(file)
			w.Write(body)
		})
	}
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		controller := http.NewResponseController(w)
		previous := ""
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			signature := "missing"
			if info, err := os.Stat(filepath.Join(dataDir, "listings.json")); err == nil {
				signature = fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
			}
			if info, e := os.Stat(filepath.Join(dataDir, "browser-listings")); e == nil {
				signature += fmt.Sprint(info.ModTime().UnixNano())
			}
			if info, err := os.Stat(filepath.Join(dataDir, "details")); err == nil {
				signature += fmt.Sprint(info.ModTime().UnixNano())
			}
			if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			var err error
			if signature != previous {
				_, err = fmt.Fprintf(w, "event: update\ndata: %q\n\n", signature)
				previous = signature
			} else {
				_, err = fmt.Fprint(w, ": heartbeat\n\n")
			}
			if err != nil {
				return
			}
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		pages := map[string][3]string{
			"/":         {"dashboard", "Хяналтын самбар", "Үл хөдлөхийн зах зээлийн цуглуулалтын тойм"},
			"/listings": {"listings", "Цуглуулсан зарууд", "Зарах, түрээслэх заруудыг хайж, дэлгэрэнгүй мэдээллийг шалгах"},
			"/phones":   {"listings", "Дугаартай зарууд", "Утасны дугаар нь хадгалагдсан зарууд болон нэмэлт мэдээлэл"},
			"/activity": {"activity", "Ажиллагааны түүх", "Crawler-ийн сүүлийн ажиллагаа болон алдааны бүртгэл"},
		}
		current, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' https://cdn1.unegui.mn https://cdn.unegui.mn; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		body, _ := assets.ReadFile("index.html")
		page := strings.NewReplacer("{{PAGE}}", current[0], "{{TITLE}}", current[1], "{{SUBTITLE}}", current[2]).Replace(string(body))
		w.Write([]byte(page))
	})
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		rows := []unegui.Listing{}
		path := filepath.Join(dataDir, "listings.json")
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "Cannot read listings", 500)
			return
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &rows); err != nil {
				http.Error(w, "Invalid listings file", 500)
				return
			}
		}
		rows = mergeBrowserListings(dataDir, rows)
		var updated *time.Time
		entries := detailCache.load(filepath.Join(dataDir, "details"))
		index := map[string]int{}
		for i, row := range rows {
			index[row.ExternalID] = i
		}
		for name, detail := range entries {
			id := strings.TrimSuffix(name, ".json")
			i, ok := index[id]
			if !ok || !detailID.MatchString(id) {
				continue
			}
			rows[i].Status = detail.Status
			rows[i].DetailComplete = detail.Complete
			rows[i].DetailCollectedAt = &detail.CollectedAt
			rows[i].Phones = detail.Phones
			rows[i].Description = detail.Description
			rows[i].Attributes = detail.Attributes
		}
		if info, err := os.Stat(path); err == nil {
			t := info.ModTime().UTC()
			updated = &t
		}
		logs := []string{}
		files, _ := filepath.Glob(filepath.Join(logDir, "crawler-*.log"))
		sort.Strings(files)
		if len(files) > 0 {
			if data, err := os.ReadFile(files[len(files)-1]); err == nil {
				text := strings.TrimPrefix(string(data), "\ufeff")
				for _, line := range strings.Split(text, "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						logs = append(logs, line)
					}
				}
				if len(logs) > 30 {
					logs = logs[len(logs)-30:]
				}
			}
		}
		var output interface{ Write([]byte) (int, error) } = w
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Add("Vary", "Accept-Encoding")
			compressed, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
			defer compressed.Close()
			output = compressed
		}
		json.NewEncoder(output).Encode(map[string]any{"items": rows, "updated_at": updated, "logs": logs, "schedule_minutes": 30})
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	return &dashboardHandler{Handler: mux, close: closeCollector, dataDir: dataDir}
}

func (h *dashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/collector/") {
		if _, err := os.Stat(filepath.Join(h.dataDir, "mining-stopped")); err == nil {
			http.Error(w, "Mining stopped by user", http.StatusForbidden)
			return
		}
	}
	h.Handler.ServeHTTP(w, r)
}

type dashboardHandler struct {
	dataDir string
	http.Handler
	close func()
}

func (h *dashboardHandler) Close() error { h.close(); return nil }
