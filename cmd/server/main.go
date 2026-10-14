// Store Doctor HTTP service. Cloud Run sets PORT; DATABASE_URL and GEMINI_API_KEY are optional.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/advisor"
	"github.com/Anirudh-Nadipally/store-doctor/internal/db"
	"github.com/Anirudh-Nadipally/store-doctor/internal/rules"
	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

//go:embed index.html
var static embed.FS

type fetcher interface {
	Fetch(ctx context.Context, handle string) (*sm.Snapshot, error)
}

type server struct {
	sm      fetcher
	adv     advisor.Advisor
	store   *db.Store // nil when no DATABASE_URL
	cacheMu sync.Mutex
	cache   map[string]cached
}

type cached struct {
	resp diagnoseResponse
	at   time.Time
}

type diagnoseResponse struct {
	rules.Report
	Advice  string     `json:"advice,omitempty"`
	History []db.Point `json:"history,omitempty"`
}

const cacheTTL = 10 * time.Minute

func (s *server) diagnose(w http.ResponseWriter, r *http.Request) {
	handle, err := sm.NormalizeHandle(r.URL.Query().Get("store"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.cacheMu.Lock()
	c, ok := s.cache[handle]
	s.cacheMu.Unlock()
	if ok && time.Since(c.at) < cacheTTL {
		writeJSON(w, c.resp)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	snap, err := s.sm.Fetch(ctx, handle)
	if errors.Is(err, sm.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "No public store called @"+handle)
		return
	} else if err != nil {
		slog.Error("fetch", "handle", handle, "err", err)
		writeErr(w, http.StatusBadGateway, "Couldn't read that store right now")
		return
	}
	rep := rules.Evaluate(snap, time.Now())
	resp := diagnoseResponse{Report: rep}
	if advice, err := s.adv.Advise(ctx, rep); err != nil {
		slog.Warn("advisor failed; returning rules-only report", "err", err)
	} else {
		resp.Advice = advice
	}
	if s.store != nil {
		if err := s.store.Save(ctx, rep, resp.Advice); err != nil {
			slog.Error("save", "err", err)
		}
		resp.History, _ = s.store.History(ctx, handle, 10)
	}
	s.cacheMu.Lock()
	s.cache[handle] = cached{resp, time.Now()}
	s.cacheMu.Unlock()
	writeJSON(w, resp)
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/diagnose", s.diagnose)
	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, _ *http.Request) {
		type rr struct {
			ID, Name, Fix string
			Points        int
		}
		var out []rr
		for _, x := range rules.Rules {
			out = append(out, rr{x.ID, x.Name, x.Fix, x.Points})
		}
		writeJSON(w, map[string]any{"version": rules.Version, "rules": out})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("GET /", http.FileServerFS(static))
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func main() {
	ctx := context.Background()
	s := &server{sm: sm.NewClient(), adv: advisor.None{}, cache: map[string]cached{}}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		s.adv = advisor.NewGemini(key)
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		st, err := db.Open(ctx, url)
		if err != nil {
			slog.Error("postgres unavailable", "err", err)
			os.Exit(1)
		}
		defer st.Close()
		s.store = st
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	slog.Info("store-doctor listening", "port", port, "postgres", s.store != nil, "llm", os.Getenv("GEMINI_API_KEY") != "")
	srv := &http.Server{Addr: ":" + port, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
