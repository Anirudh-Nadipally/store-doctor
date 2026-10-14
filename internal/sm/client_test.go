package sm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestParseRealStorePage(t *testing.T) {
	html, err := os.ReadFile("testdata/unixstickers.html") // saved 2026-10-14
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseStorePage("unixstickers", html)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Unixstickers" || !s.HasAvatar || !s.HasBanner || !s.Verified {
		t.Fatalf("profile fields wrong: %+v", s)
	}
	if s.ItemCount != PageItemLimit || s.ProductTypes < 2 {
		t.Fatalf("items: %d types: %d", s.ItemCount, s.ProductTypes)
	}
	if s.Orders != 956 || s.JoinDate.Year() != 2023 {
		t.Fatalf("orders %d join %v", s.Orders, s.JoinDate)
	}
}

func TestNormalizeHandle(t *testing.T) {
	for in, want := range map[string]string{
		"@Unixstickers": "unixstickers", "https://www.stickermule.com/unixstickers/": "unixstickers",
		"stickermule.com/claudecode?x=1": "claudecode",
	} {
		got, err := NormalizeHandle(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v", in, got, err)
		}
	}
	if _, err := NormalizeHandle("../etc/passwd"); err == nil {
		t.Error("path traversal accepted")
	}
}

func TestLeaderboardDecodesAndSurfacesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"publicProfiles":{"users":[{"vanityName":"a","salesCount":3}]}}}`))
	}))
	defer srv.Close()
	c := NewClient()
	c.Base, c.MinDelay = srv.URL, 0
	got, err := c.Leaderboard(context.Background(), 0, 1)
	if err != nil || len(got) != 1 || got[0].Orders != 3 {
		t.Fatalf("%v %v", got, err)
	}
}
