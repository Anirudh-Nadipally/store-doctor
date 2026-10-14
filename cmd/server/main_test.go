package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Anirudh-Nadipally/store-doctor/internal/advisor"
	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

type fakeFetcher struct{ calls int }

func (f *fakeFetcher) Fetch(_ context.Context, h string) (*sm.Snapshot, error) {
	f.calls++
	if h == "missing" {
		return nil, sm.ErrNotFound
	}
	return &sm.Snapshot{Handle: h, Name: "Shop", ItemCount: 2}, nil
}

func TestDiagnoseEndpoint(t *testing.T) {
	f := &fakeFetcher{}
	s := &server{sm: f, adv: advisor.None{}, cache: map[string]cached{}}
	h := s.routes()

	get := func(q string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/diagnose?store="+q, nil))
		return rec
	}
	rec := get("@Shop")
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Handle string
		Score  int
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Handle != "shop" || out.Score == 0 {
		t.Fatalf("%+v", out)
	}
	get("shop")
	if f.calls != 1 {
		t.Fatalf("second request should hit cache, fetched %d times", f.calls)
	}
	if get("missing").Code != http.StatusNotFound || get("bad!handle").Code != http.StatusBadRequest {
		t.Fatal("error codes wrong")
	}
}
