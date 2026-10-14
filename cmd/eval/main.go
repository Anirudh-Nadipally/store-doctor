// eval checks whether the Store Doctor score separates stores that sell from stores that don't.
// Labels come from the public leaderboard (orders); features come only from the store page.
//
//	go run ./cmd/eval -n 60 -seed 7 -out eval
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/rules"
	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

type row struct {
	Group  string
	Handle string
	Orders int
	Report rules.Report
}

func main() {
	n := flag.Int("n", 60, "stores per group")
	seed := flag.Int64("seed", 7, "random seed")
	out := flag.String("out", "eval", "output directory")
	flag.Parse()

	c := sm.NewClient()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(*seed))

	// Leaderboard ranks, probed 2026-10-14: offsets < 1000 have >= 5 orders; offsets >= 8000 have 0.
	groups := []struct {
		name   string
		lo, hi int
		accept func(int) bool
	}{
		{"sellers", 0, 1000, func(o int) bool { return o >= 5 }},
		{"zero", 8000, 50000, func(o int) bool { return o == 0 }},
	}
	var rows []row
	for _, g := range groups {
		got, tries := 0, 0
		seen := map[string]bool{}
		for got < *n && tries < *n*3 {
			tries++
			off := g.lo + rng.Intn(g.hi-g.lo)
			lb, err := c.Leaderboard(ctx, off, 1)
			if err != nil || len(lb) == 0 || !g.accept(lb[0].Orders) || seen[lb[0].Handle] {
				continue
			}
			seen[lb[0].Handle] = true
			snap, err := c.Fetch(ctx, lb[0].Handle)
			if err != nil {
				log.Printf("skip %s: %v", lb[0].Handle, err)
				continue
			}
			rows = append(rows, row{g.name, lb[0].Handle, lb[0].Orders, rules.Evaluate(snap, time.Now())})
			got++
			fmt.Fprintf(os.Stderr, "\r%s %d/%d", g.name, got, *n)
		}
		fmt.Fprintln(os.Stderr)
	}

	res := summarize(rows)
	res["seed"], res["n_per_group"], res["run_at"] = *seed, *n, time.Now().UTC().Format(time.RFC3339)
	os.MkdirAll(*out, 0o755)
	js, _ := json.MarshalIndent(res, "", "  ")
	os.WriteFile(filepath.Join(*out, "results.json"), js, 0o644)
	writeCSV(filepath.Join(*out, "sample.csv"), rows)
	fmt.Println(string(js))
}

func summarize(rows []row) map[string]any {
	var pos, neg, posStocked, negStocked []float64
	for _, r := range rows {
		s := float64(r.Report.Score)
		stocked := r.Report.Snapshot.ItemCount > 0
		if r.Group == "sellers" {
			pos = append(pos, s)
			if stocked {
				posStocked = append(posStocked, s)
			}
		} else {
			neg = append(neg, s)
			if stocked {
				negStocked = append(negStocked, s)
			}
		}
	}
	type ruleStat struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		PassSellers float64 `json:"pass_rate_sellers"`
		PassZero    float64 `json:"pass_rate_zero"`
		Gap         float64 `json:"gap_pp"`
		Verdict     string  `json:"verdict"`
	}
	var stats []ruleStat
	for i, rule := range rules.Rules {
		var ps, pz, ns, nz float64
		for _, r := range rows {
			pass := r.Report.Results[i].Passed
			if r.Group == "sellers" {
				ns++
				if pass {
					ps++
				}
			} else {
				nz++
				if pass {
					pz++
				}
			}
		}
		st := ruleStat{ID: rule.ID, Name: rule.Name, PassSellers: round(100 * ps / ns), PassZero: round(100 * pz / nz)}
		st.Gap = round(st.PassSellers - st.PassZero)
		switch {
		case st.Gap >= 20:
			st.Verdict = "keep: strong signal"
		case st.Gap >= 10:
			st.Verdict = "keep: weak signal"
		default:
			st.Verdict = "remove or rethink: does not separate sellers"
		}
		stats = append(stats, st)
	}
	return map[string]any{
		"auc_all":               round3(auc(pos, neg)),
		"auc_stores_with_items": round3(auc(posStocked, negStocked)),
		"mean_score_sellers":    round(mean(pos)),
		"mean_score_zero":       round(mean(neg)),
		"n_sellers":             len(pos),
		"n_zero":                len(neg),
		"n_sellers_with_items":  len(posStocked),
		"n_zero_with_items":     len(negStocked),
		"zero_sale_empty_share": round(100 * float64(len(neg)-len(negStocked)) / float64(max(1, len(neg)))),
		"rules":                 stats,
	}
}

// auc is the probability a random seller outscores a random zero-sale store (ties count half).
func auc(pos, neg []float64) float64 {
	if len(pos) == 0 || len(neg) == 0 {
		return 0
	}
	var w float64
	for _, p := range pos {
		for _, n := range neg {
			switch {
			case p > n:
				w++
			case p == n:
				w += 0.5
			}
		}
	}
	return w / float64(len(pos)*len(neg))
}

func mean(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

func round(f float64) float64  { return float64(int(f*10+0.5)) / 10 }
func round3(f float64) float64 { return float64(int(f*1000+0.5)) / 1000 }

func writeCSV(path string, rows []row) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	header := []string{"group", "handle", "orders", "score", "items", "product_types"}
	for _, r := range rules.Rules {
		header = append(header, r.ID)
	}
	w.Write(header)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Orders > rows[j].Orders })
	for _, r := range rows {
		rec := []string{r.Group, r.Handle, strconv.Itoa(r.Orders), strconv.Itoa(r.Report.Score),
			strconv.Itoa(r.Report.Snapshot.ItemCount), strconv.Itoa(r.Report.Snapshot.ProductTypes)}
		for _, res := range r.Report.Results {
			rec = append(rec, map[bool]string{true: "1", false: "0"}[res.Passed])
		}
		w.Write(rec)
	}
	w.Flush()
}
