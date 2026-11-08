package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/temoto/robotstxt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"realty-data-miner/internal/unegui"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const agent = "RealtyDataMiner/0.1"
const origin = "https://www.unegui.mn"

type crawler struct {
	report *runReport
	full   bool
	client *http.Client
	delay  time.Duration
	next   time.Time
	rules  *robotstxt.Group
}

type progress struct {
	Source     string    `json:"source"`
	NextPage   int       `json:"next_page"`
	TotalPages int       `json:"total_pages"`
	Completed  bool      `json:"completed"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func checkpointPath(out, seed string) string {
	return filepath.Join(out, "full-"+unegui.TransactionType(seed)+".json")
}

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *crawler) get(ctx context.Context, target string) ([]byte, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || u.Host != "www.unegui.mn" {
		return nil, fmt.Errorf("URL outside allowed origin")
	}
	if c.rules != nil && !c.rules.Test(u.RequestURI()) {
		return nil, fmt.Errorf("robots.txt disallows %s", u.RequestURI())
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := pause(ctx, time.Until(c.next)); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", agent)
		resp, err := c.client.Do(req)
		c.next = time.Now().Add(c.delay)
		if err != nil {
			last = err
			c.next = time.Now().Add(c.delay * time.Duration(1<<attempt))
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			last = fmt.Errorf("HTTP %d: %s", resp.StatusCode, target)
			if resp.StatusCode != 429 && resp.StatusCode < 500 {
				return nil, last
			}
			backoff := c.delay * time.Duration(1<<attempt)
			retry := resp.Header.Get("Retry-After")
			if sec, e := strconv.Atoi(retry); e == nil && sec > 0 {
				backoff = max(backoff, time.Duration(sec)*time.Second)
			} else if date, e := http.ParseTime(retry); e == nil {
				backoff = max(backoff, time.Until(date))
			}
			c.next = time.Now().Add(backoff)
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
		resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if len(body) > 10<<20 {
			return nil, fmt.Errorf("response exceeds 10 MiB")
		}
		return body, nil
	}
	return nil, last
}

func save(path string, rows []unegui.Listing) error {
	return saveJSON(path, rows)
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".listings-*.json")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func cycle(ctx context.Context, c *crawler, seed, out string, pages int) error {
	state := progress{Source: seed, NextPage: 1}
	if c.full {
		data, e := os.ReadFile(checkpointPath(out, seed))
		if e == nil {
			if e = json.Unmarshal(data, &state); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if state.Completed {
			log.Printf("full crawl already completed: %s", seed)
			return nil
		}
		if state.Source != seed || state.NextPage < 1 {
			return fmt.Errorf("invalid checkpoint")
		}
	}
	c.rules = nil
	body, err := c.get(ctx, origin+"/robots.txt")
	if err != nil {
		return fmt.Errorf("robots.txt: %w", err)
	}
	robots, err := robotstxt.FromBytes(body)
	if err != nil {
		return err
	}
	c.rules = robots.FindGroup("RealtyDataMiner")
	if c.rules.CrawlDelay > c.delay {
		c.delay = c.rules.CrawlDelay
	}
	if err := os.MkdirAll(out, 0750); err != nil {
		return err
	}
	path := filepath.Join(out, "listings.json")
	rows := []unegui.Listing{}
	if data, err := os.ReadFile(path); err == nil {
		if err = json.Unmarshal(data, &rows); err != nil {
			return fmt.Errorf("existing listings: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	index := map[string]int{}
	for i, row := range rows {
		index[row.ExternalID] = i
	}
	total := 0
	totalNew := 0
	start := 1
	if c.full {
		start = state.NextPage
		pages = max(state.TotalPages, start)
	}
	for page := start; page <= pages; page++ {
		u, _ := url.Parse(seed)
		q := u.Query()
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		u.RawQuery = q.Encode()
		body, err := c.get(ctx, u.String())
		if err != nil {
			return err
		}
		listings, err := unegui.Parse(bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("page %d: %w", page, err)
		}
		if c.full {
			count, e := unegui.PageCount(bytes.NewReader(body), seed)
			if e != nil {
				return e
			}
			pages = max(pages, count)
			state.TotalPages = pages
		}
		now := time.Now().UTC()
		pageNew := 0
		for _, row := range listings {
			row.TransactionType = unegui.TransactionType(seed)
			row.FirstSeen = now
			row.LastSeen = now
			if i, ok := index[row.ExternalID]; ok {
				if row.TransactionType == "" {
					row.TransactionType = rows[i].TransactionType
				}
				row.FirstSeen = rows[i].FirstSeen
				rows[i] = row
			} else {
				index[row.ExternalID] = len(rows)
				pageNew++
				rows = append(rows, row)
			}
		}
		if err := save(path, rows); err != nil {
			return err
		}
		if c.full {
			state.NextPage = page + 1
			state.Completed = page >= pages
			state.UpdatedAt = time.Now().UTC()
			if err := saveJSON(checkpointPath(out, seed), state); err != nil {
				return err
			}
		}
		if c.report != nil {
			detail := pageReport{Category: unegui.TransactionType(seed), Page: page, Observed: len(listings)}
			c.report.Pages++
			c.report.Category = unegui.TransactionType(seed)
			c.report.Page = page
			for _, row := range listings {
				c.report.Observed++
				if c.report.known[row.ExternalID] {
					c.report.Existing++
					detail.Existing++
				} else {
					c.report.New++
					detail.New++
					detail.NewIDs = append(detail.NewIDs, row.ExternalID)
					c.report.known[row.ExternalID] = true
				}
			}
			c.report.PageDetails = append(c.report.PageDetails, detail)
			if err := c.report.write(); err != nil {
				return fmt.Errorf("run report: %w", err)
			}
		}
		total += len(listings)
		totalNew += pageNew
		log.Printf("category=%s page=%d/%d observed=%d new_to_file=%d existing=%d stored=%d", unegui.TransactionType(seed), page, pages, len(listings), pageNew, len(listings)-pageNew, len(rows))
	}
	log.Printf("crawl complete observed=%d new_to_file=%d unique_stored=%d", total, totalNew, len(rows))
	return nil
}

func run() error {
	seed := flag.String("url", origin+"/l-hdlh/", "Unegui real estate category URL")
	out := flag.String("out", "data", "Persistent data directory")
	pages := flag.Int("pages", 1, "Maximum category pages per run")
	delay := flag.Duration("delay", 5*time.Second, "Minimum request delay")
	interval := flag.Duration("interval", 0, "Repeat after this duration; zero runs once")
	full := flag.Bool("full", false, "Crawl every category page, resuming saved progress")
	flag.Parse()
	if _, err := os.Stat(filepath.Join(*out, "mining-stopped")); err == nil {
		return fmt.Errorf("mining stopped by user")
	}
	u, err := url.Parse(*seed)
	if err != nil || u.Scheme != "https" || u.Host != "www.unegui.mn" || !strings.HasPrefix(u.Path, "/l-hdlh/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("url must be an HTTPS www.unegui.mn/l-hdlh/ category without query or fragment")
	}
	if *pages < 1 || *pages > 100 || *delay < time.Second || *interval < 0 {
		return fmt.Errorf("pages must be 1..100, delay >= 1s, interval >= 0")
	}
	if *full && unegui.TransactionType(*seed) == "" && u.Path != "/l-hdlh/" {
		return fmt.Errorf("full crawl requires sale/rent category")
	}
	lock, err := net.Listen("tcp", "127.0.0.1:18081")
	if err != nil {
		log.Print("another crawler is running; skipping this run")
		return nil
	}
	defer lock.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	c := &crawler{client: client, delay: *delay, full: *full}
	for {
		seeds := []string{*seed}
		if u.Path == "/l-hdlh/" {
			seeds = []string{origin + "/l-hdlh/l-hdlh-zarna/", origin + "/l-hdlh/l-hdlh-treesllne/"}
		}
		report, reportErr := beginReport(*out, *pages)
		if reportErr != nil {
			return reportErr
		}
		c.report = report
		var err error
		for _, source := range seeds {
			if e := cycle(ctx, c, source, *out, *pages); e != nil {
				log.Printf("category failed: %v", e)
				err = errors.Join(err, e)
				report.Errors = append(report.Errors, e.Error())
			}
		}
		status := "completed"
		if err != nil {
			status = "failed"
		}
		if ctx.Err() != nil {
			status = "interrupted"
		}
		if e := report.finish(status); e != nil {
			err = errors.Join(err, e)
		}
		if ctx.Err() != nil {
			return nil
		}
		if *interval == 0 {
			return err
		}
		if err != nil {
			log.Printf("crawl failed: %v", err)
		}
		if err := pause(ctx, max(*interval, c.delay)); err != nil {
			return nil
		}
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
