package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFolderCacheRefresh(t *testing.T) {
	dir := t.TempDir()
	var c folderCache[Detail]
	write := func(value string) {
		t.Helper()
		f, e := os.CreateTemp(dir, ".tmp")
		if e != nil {
			t.Fatal(e)
		}
		f.WriteString(value)
		f.Close()
		if e = os.Rename(f.Name(), filepath.Join(dir, "1.json")); e != nil {
			t.Fatal(e)
		}
	}
	write(`{"external_id":"1","complete":false}`)
	if c.load(dir)["1.json"].Complete {
		t.Fatal("initial")
	}
	time.Sleep(10 * time.Millisecond)
	write(`{"external_id":"1","complete":true}`)
	if !c.load(dir)["1.json"].Complete {
		t.Fatal("stale cache")
	}
	os.Remove(filepath.Join(dir, "1.json"))
	if len(c.load(dir)) != 0 {
		t.Fatal("deleted file retained")
	}
}
