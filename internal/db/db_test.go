package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/rules"
	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

// Runs only when TEST_DATABASE_URL points at a disposable Postgres (CI starts one).
func TestSaveAndHistory(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := "test-" + time.Now().Format("150405.000000")
	for _, items := range []int{0, 6} {
		r := rules.Evaluate(&sm.Snapshot{Handle: h, Name: "T", ItemCount: items}, time.Now())
		if err := s.Save(ctx, r, ""); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := s.History(ctx, h, 10)
	if err != nil || len(hist) != 2 || hist[0].Score <= hist[1].Score {
		t.Fatalf("history %v %v", hist, err)
	}
}
