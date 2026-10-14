package rules

import (
	"testing"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

func TestPointsSumTo100(t *testing.T) {
	total := 0
	seen := map[string]bool{}
	for _, r := range Rules {
		if seen[r.ID] {
			t.Fatalf("duplicate rule id %s", r.ID)
		}
		seen[r.ID] = true
		total += r.Points
	}
	if total != 100 {
		t.Fatalf("rule points sum to %d, want 100", total)
	}
}

func TestEmptyStore(t *testing.T) {
	s := &sm.Snapshot{Handle: "0c5c41e78a22392", Name: "0c5c41e78a22392"}
	r := Evaluate(s, time.Now())
	if r.Score != 0 || r.Grade != "Critical" {
		t.Fatalf("got %d %s", r.Score, r.Grade)
	}
	if len(r.TopFixes) != 3 || r.TopFixes[0] != Rules[4].Fix {
		t.Fatalf("top fix should be 'has products', got %v", r.TopFixes)
	}
}

func TestHealthyStoreIsDeterministic(t *testing.T) {
	s := &sm.Snapshot{Handle: "unixstickers", Name: "Unixstickers", HasAvatar: true, HasBanner: true,
		Bio: "Welcome to the original Unixstickers store! Since 2008.", Verified: true, ItemCount: 20, ProductTypes: 4}
	a, b := Evaluate(s, time.Now()), Evaluate(s, time.Now())
	if a.Score != 100 || a.Score != b.Score || len(a.TopFixes) != 0 {
		t.Fatalf("got %d / %d, fixes %v", a.Score, b.Score, a.TopFixes)
	}
}
