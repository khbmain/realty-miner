package unegui

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Listing struct {
	Status            string            `json:"status,omitempty"`
	DetailComplete    bool              `json:"detail_complete"`
	DetailCollectedAt *time.Time        `json:"detail_collected_at,omitempty"`
	Phones            []string          `json:"phones,omitempty"`
	Description       string            `json:"description,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
	TransactionType   string            `json:"transaction_type,omitempty"`
	Source            string            `json:"source"`
	ExternalID        string            `json:"external_id"`
	URL               string            `json:"url"`
	Title             string            `json:"title"`
	PriceText         string            `json:"price_text"`
	Price             *float64          `json:"price,omitempty"`
	Currency          string            `json:"currency"`
	ImageURL          string            `json:"image_url,omitempty"`
	Location          string            `json:"location,omitempty"`
	PublishedText     string            `json:"published_text,omitempty"`
	FirstSeen         time.Time         `json:"first_seen"`
	LastSeen          time.Time         `json:"last_seen"`
}

// TransactionType uses the source category, never a price or title guess.
func TransactionType(sourceURL string) string {
	u, err := url.Parse(sourceURL)
	if err != nil {
		return ""
	}
	if strings.HasPrefix(u.Path, "/l-hdlh/l-hdlh-zarna/") {
		return "sale"
	}
	if strings.HasPrefix(u.Path, "/l-hdlh/l-hdlh-treesllne/") {
		return "rent"
	}
	return ""
}

var adPath = regexp.MustCompile(`^/adv/([0-9]+)_[^/]+/$`)
var priceNumber = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// PageCount follows the category's own pagination links.
func PageCount(r io.Reader, sourceURL string) (int, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return 0, err
	}
	base, err := url.Parse(sourceURL)
	if err != nil {
		return 0, err
	}
	pages := 1
	doc.Find(`a[href]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		u, e := url.Parse(href)
		if e != nil || u.Path != base.Path || (u.Host != "" && u.Host != base.Host) {
			return
		}
		if n, e := strconv.Atoi(u.Query().Get("page")); e == nil && n > pages {
			pages = n
		}
	})
	return pages, nil
}

func ParsePrice(raw string) *float64 {
	s := strings.ToLower(strings.TrimSpace(raw))
	if !strings.Contains(s, "₮") {
		return nil
	}
	multiplier := 1.0
	if strings.Contains(s, "тэрбум") {
		multiplier = 1e9
	} else if strings.Contains(s, "сая") {
		multiplier = 1e6
	}
	for _, part := range []string{"₮", "тэрбум", "сая", ",", " ", "\u00a0", "\u202f"} {
		s = strings.ReplaceAll(s, part, "")
	}
	if !priceNumber.MatchString(s) {
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	n *= multiplier
	return &n
}

// Parse reads visible listing cards, excluding scripts and author profiles.
func Parse(r io.Reader) ([]Listing, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	result := []Listing{}
	seen := map[string]bool{}
	doc.Find(`[itemscope][itemtype="http://schema.org/Product"], [itemscope][itemtype="https://schema.org/Product"]`).Each(func(_ int, card *goquery.Selection) {
		href, _ := card.Find(`a[itemprop="url"]`).First().Attr("href")
		u, e := url.Parse(href)
		if e != nil || u.Host != "" {
			return
		}
		match := adPath.FindStringSubmatch(u.Path)
		if len(match) != 2 || seen[match[1]] {
			return
		}
		title, _ := card.Find(`meta[itemprop="name"]`).Attr("content")
		if strings.TrimSpace(title) == "" {
			return
		}
		raw, _ := card.Find(`meta[itemprop="price"]`).Attr("content")
		currency, _ := card.Find(`meta[itemprop="priceCurrency"]`).Attr("content")
		image, _ := card.Find(`meta[itemprop="image"]`).Attr("content")
		alt, _ := card.Find("img").First().Attr("alt")
		location := strings.TrimPrefix(alt, title+", ")
		if location == alt {
			location = ""
		}
		var published string
		card.Find("p").Each(func(_ int, p *goquery.Selection) {
			text := strings.TrimSpace(p.Text())
			if location != "" && strings.Contains(text, location) {
				published = strings.TrimSpace(strings.TrimSuffix(text, location))
			}
		})
		seen[match[1]] = true
		result = append(result, Listing{Source: "unegui.mn", ExternalID: match[1], URL: "https://www.unegui.mn" + u.Path, Title: title, PriceText: raw, Price: ParsePrice(raw), Currency: currency, ImageURL: image, Location: location, PublishedText: published})
	})
	if len(result) == 0 {
		return nil, fmt.Errorf("no listing cards found: page structure changed, empty category or access challenge")
	}
	return result, nil
}
