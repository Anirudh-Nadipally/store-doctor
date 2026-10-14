// Package advisor optionally asks an LLM to turn failed rules into store-specific advice.
// It receives the finished report and returns text only: it cannot change the score.
package advisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/rules"
)

type Advisor interface {
	Advise(ctx context.Context, r rules.Report) (string, error)
}

// None is used when no API key is configured.
type None struct{}

func (None) Advise(context.Context, rules.Report) (string, error) { return "", nil }

// Gemini calls the Gemini generateContent REST API.
type Gemini struct {
	APIKey   string
	Model    string
	Endpoint string // override for tests
	HTTP     *http.Client
}

func NewGemini(key string) *Gemini {
	return &Gemini{APIKey: key, Model: "gemini-2.5-flash",
		Endpoint: "https://generativelanguage.googleapis.com/v1beta/models", HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func Prompt(r rules.Report) string {
	var b strings.Builder
	s := r.Snapshot
	fmt.Fprintf(&b, "You advise a seller on Sticker Mule's marketplace. Store @%s, name %q, bio %q, %d items across %d product kinds.\n",
		s.Handle, s.Name, s.Bio, s.ItemCount, s.ProductTypes)
	fmt.Fprintf(&b, "A rules engine scored the store %d/100. Failed checks:\n", r.Score)
	for _, res := range r.Results {
		if !res.Passed {
			fmt.Fprintf(&b, "- %s %s (%s)\n", res.ID, res.Name, res.Detail)
		}
	}
	b.WriteString("Write at most 3 concrete actions for this specific store, one line each, highest impact first. " +
		"If the bio is weak, include a rewritten bio under 160 characters. Do not mention scores. Plain text, no markdown.")
	return b.String()
}

func (g *Gemini) Advise(ctx context.Context, r rules.Report) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"contents":         []any{map[string]any{"parts": []any{map[string]string{"text": Prompt(r)}}}},
		"generationConfig": map[string]any{"temperature": 0.4, "maxOutputTokens": 400},
	})
	url := fmt.Sprintf("%s/%s:generateContent", g.Endpoint, g.Model)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.APIKey)
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct{ Text string } `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != nil {
		return "", errors.New(out.Error.Message)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("empty model response")
	}
	return strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text), nil
}
