package dashboard

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"strings"
	"sync"
	"time"
)

type cachedEntry[T any] struct {
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
	Value    T         `json:"value"`
}
type folderCache[T any] struct {
	mu      sync.Mutex
	folders map[string]map[string]cachedEntry[T]
	stamps  map[string]time.Time
}

// Files are committed by atomic rename; the directory timestamp invalidates snapshots.
func (c *folderCache[T]) load(dir string) map[string]T {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.folders == nil {
		c.folders = map[string]map[string]cachedEntry[T]{}
		c.stamps = map[string]time.Time{}
	}
	info, e := os.Stat(dir)
	if e != nil {
		return map[string]T{}
	}
	old := c.folders[dir]
	if old == nil {
		var snapshot struct {
			Stamp   time.Time                 `json:"stamp"`
			Entries map[string]cachedEntry[T] `json:"entries"`
		}
		if b, e := os.ReadFile(dir + ".cache.json"); e == nil && json.Unmarshal(b, &snapshot) == nil {
			old = snapshot.Entries
			c.folders[dir] = old
			c.stamps[dir] = snapshot.Stamp
		}
	}

	if old == nil || !info.ModTime().Equal(c.stamps[dir]) {
		fresh := map[string]cachedEntry[T]{}
		files, e := os.ReadDir(dir)
		if e != nil {
			return map[string]T{}
		}
		started := time.Now()
		type item struct {
			name  string
			entry cachedEntry[T]
		}
		jobs := make(chan os.DirEntry)
		results := make(chan item, 16)
		var workers sync.WaitGroup
		for i := 0; i < 16; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for f := range jobs {
					if !strings.HasSuffix(f.Name(), ".json") {
						continue
					}
					stat, e := f.Info()
					if e != nil {
						continue
					}
					entry, ok := old[f.Name()]
					if !ok || !entry.Modified.Equal(stat.ModTime()) || entry.Size != stat.Size() {
						b, e := os.ReadFile(filepath.Join(dir, f.Name()))
						if e != nil {
							continue
						}
						var v T
						if json.Unmarshal(b, &v) != nil {
							continue
						}
						entry = cachedEntry[T]{stat.ModTime(), stat.Size(), v}
					}
					results <- item{f.Name(), entry}
				}
			}()
		}
		go func() {
			for _, f := range files {
				jobs <- f
			}
			close(jobs)
			workers.Wait()
			close(results)
		}()
		for result := range results {
			fresh[result.name] = result.entry
		}
		// A snapshot is disposable; authoritative per-listing files remain unchanged.
		snapshot := struct {
			Stamp   time.Time                 `json:"stamp"`
			Entries map[string]cachedEntry[T] `json:"entries"`
		}{info.ModTime(), fresh}
		if data, err := json.Marshal(snapshot); err == nil {
			if file, err := os.CreateTemp(filepath.Dir(dir), ".cache-*"); err == nil {
				name := file.Name()
				_, writeErr := file.Write(data)
				closeErr := file.Close()
				if writeErr == nil && closeErr == nil {
					_ = os.Rename(name, dir+".cache.json")
				}
				_ = os.Remove(name)
			}
		}
		log.Printf("Loaded %s: %d records in %s", filepath.Base(dir), len(fresh), time.Since(started))
		c.folders[dir] = fresh
		c.stamps[dir] = info.ModTime()
		old = fresh
	}
	result := make(map[string]T, len(old))
	for name, e := range old {
		result[name] = e.Value
	}
	return result
}

var browserCache folderCache[unegui.Listing]
var detailCache folderCache[Detail]

// put updates a committed file without rescanning the directory.
func (c *folderCache[T]) put(dir, name string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.folders == nil || c.folders[dir] == nil {
		return
	}
	stat, e := os.Stat(filepath.Join(dir, name))
	if e != nil {
		return
	}
	c.folders[dir][name] = cachedEntry[T]{stat.ModTime(), stat.Size(), value}
	if info, e := os.Stat(dir); e == nil {
		c.stamps[dir] = info.ModTime()
	}
}
