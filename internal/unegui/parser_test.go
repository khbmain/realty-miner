package unegui

import (
	"os"
	"strings"
	"testing"
)

func TestPrices(t *testing.T) {
	for raw, want := range map[string]float64{"248 сая ₮": 248e6, "1.08 Тэрбум ₮": 1.08e9, "450,000 ₮": 450000, "7.5 сая ₮": 7.5e6} {
		got := ParsePrice(raw)
		if got == nil || *got != want {
			t.Errorf("%s: %v", raw, got)
		}
	}
	for _, raw := range []string{"Тохиролцоно", "1.5 сая $", "10 ₮/м²"} {
		if ParsePrice(raw) != nil {
			t.Errorf("unexpected price for %s", raw)
		}
	}
}

func TestTransactionType(t *testing.T) {
	for source, want := range map[string]string{
		"https://www.unegui.mn/l-hdlh/l-hdlh-zarna/":     "sale",
		"https://www.unegui.mn/l-hdlh/l-hdlh-treesllne/": "rent",
		"https://www.unegui.mn/l-hdlh/":                  "",
	} {
		if got := TransactionType(source); got != want {
			t.Errorf("%s: got %s", source, got)
		}
	}
}

func TestCapturedListingPage(t *testing.T) {
	f, err := os.Open("../../testdata/list.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 20 {
		t.Fatalf("too few listings: %d", len(rows))
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.ExternalID] || row.Title == "" || !strings.HasPrefix(row.URL, "https://www.unegui.mn/adv/") {
			t.Fatalf("invalid listing: %+v", row)
		}
		seen[row.ExternalID] = true
	}
	if rows[0].Price == nil || rows[0].Location == "" {
		t.Fatalf("missing price/location: %+v", rows[0])
	}
	t.Logf("parsed %d unique listings", len(rows))
}

func TestChallengeDoesNotEraseData(t *testing.T) {
	if _, err := Parse(strings.NewReader("<html><title>Access denied</title></html>")); err == nil {
		t.Fatal("expected parser failure")
	}
}
