package advisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/rules"
	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

func TestGeminiAdviceNeverChangesScore(t *testing.T) {
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" || !strings.HasSuffix(r.URL.Path, ":generateContent") {
			t.Errorf("bad request %s", r.URL.Path)
		}
		var in struct {
			Contents []struct{ Parts []struct{ Text string } } `json:"contents"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		gotPrompt = in.Contents[0].Parts[0].Text
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":" Add a banner. Score: 100/100 "}]}}]}`))
	}))
	defer srv.Close()
	g := NewGemini("k")
	g.Endpoint = srv.URL

	rep := rules.Evaluate(&sm.Snapshot{Handle: "beddbug", Name: "Beddbug", ItemCount: 1}, time.Now())
	before := rep.Score
	txt, err := g.Advise(context.Background(), rep)
	if err != nil || txt != "Add a banner. Score: 100/100" {
		t.Fatalf("%q %v", txt, err)
	}
	if rep.Score != before {
		t.Fatal("advice changed the score")
	}
	if !strings.Contains(gotPrompt, "SD-03 Banner image") || strings.Contains(gotPrompt, "SD-06") {
		t.Fatalf("prompt should list only failed rules:\n%s", gotPrompt)
	}
}
