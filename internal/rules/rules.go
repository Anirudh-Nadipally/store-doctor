// Package rules turns a store snapshot into a score. Pure code: same snapshot in,
// same report out. An LLM may phrase advice later, but it never touches the score.
package rules

import (
	"regexp"
	"time"

	"github.com/Anirudh-Nadipally/store-doctor/internal/sm"
)

// v1.1.0 removed SD-05 (display name) and SD-10 (store is public): in the v1.0 eval both
// passed for 100% of sellers and 100% of zero-sale stores, so they added points without signal.
const Version = "1.1.0"

type Rule struct {
	ID     string
	Name   string
	Points int
	Check  func(s *sm.Snapshot, now time.Time) (bool, string)
	Fix    string
}

type Result struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Points int    `json:"points"` // earned
	Max    int    `json:"max"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"` // only on failed rules
}

type Report struct {
	Handle       string       `json:"handle"`
	Score        int          `json:"score"` // 0-100
	Grade        string       `json:"grade"`
	RulesVersion string       `json:"rulesVersion"`
	Results      []Result     `json:"results"`
	TopFixes     []string     `json:"topFixes"`
	Snapshot     *sm.Snapshot `json:"snapshot"`
}

// autoHandle matches the 15-char hex handles the site assigns when a seller never picks one.
var autoHandle = regexp.MustCompile(`^[0-9a-f]{15}$`)

// Rules is the active rule set. Points are the max each rule can add; they sum to 100.
var Rules = []Rule{
	{"SD-01", "Custom store handle", 14, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		if autoHandle.MatchString(s.Handle) {
			return false, "handle @" + s.Handle + " is auto-generated"
		}
		return true, "@" + s.Handle
	}, "Pick a short, memorable handle. An auto-generated ID can't be said out loud or shared."},

	{"SD-02", "Profile picture", 12, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		return s.HasAvatar, yesNo(s.HasAvatar, "avatar set", "no avatar")
	}, "Upload a logo or face as the store avatar so buyers recognise the brand."},

	{"SD-03", "Banner image", 12, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		return s.HasBanner, yesNo(s.HasBanner, "banner set", "no banner")
	}, "Add a banner that shows your best design: it's the first thing a visitor sees."},

	{"SD-04", "Store bio", 12, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		n := len([]rune(s.Bio))
		return n >= 40, itoa(n) + " characters"
	}, "Write a 1-2 sentence bio: who you are and why someone should buy from you."},

	{"SD-06", "Has products", 18, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		return s.ItemCount >= 1, itoa(s.ItemCount) + " items listed"
	}, "List at least one product. An empty store can't sell."},

	{"SD-07", "Catalog depth (5+ items)", 14, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		return s.ItemCount >= 5, itoa(s.ItemCount) + " items"
	}, "Grow to 5+ designs. Buyers who like one design usually browse for a second."},

	{"SD-08", "Product variety (2+ kinds)", 9, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		return s.ProductTypes >= 2, itoa(s.ProductTypes) + " product kinds"
	}, "Offer a second product kind (e.g. sticker sheets or magnets) using the same artwork."},

	{"SD-09", "Identity verified", 9, func(s *sm.Snapshot, _ time.Time) (bool, string) {
		return s.Verified, yesNo(s.Verified, "verified", "not verified")
	}, "Verify your identity to get the badge buyers look for."},
}

func Evaluate(s *sm.Snapshot, now time.Time) Report {
	r := Report{Handle: s.Handle, RulesVersion: Version, Snapshot: s}
	type fix struct {
		pts int
		txt string
	}
	var fixes []fix
	for _, rule := range Rules {
		ok, detail := rule.Check(s, now)
		res := Result{ID: rule.ID, Name: rule.Name, Passed: ok, Max: rule.Points, Detail: detail}
		if ok {
			res.Points = rule.Points
			r.Score += rule.Points
		} else {
			res.Fix = rule.Fix
			fixes = append(fixes, fix{rule.Points, rule.Fix})
		}
		r.Results = append(r.Results, res)
	}
	// Biggest missing points first; stable so ties keep rule order.
	for i := 1; i < len(fixes); i++ {
		for j := i; j > 0 && fixes[j].pts > fixes[j-1].pts; j-- {
			fixes[j], fixes[j-1] = fixes[j-1], fixes[j]
		}
	}
	for i := 0; i < len(fixes) && i < 3; i++ {
		r.TopFixes = append(r.TopFixes, fixes[i].txt)
	}
	r.Grade = grade(r.Score)
	return r
}

func grade(score int) string {
	switch {
	case score >= 85:
		return "Healthy"
	case score >= 60:
		return "Needs work"
	default:
		return "Critical"
	}
}

func yesNo(ok bool, y, n string) string {
	if ok {
		return y
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
