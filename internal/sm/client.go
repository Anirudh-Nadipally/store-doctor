// Package sm reads public Sticker Mule store data: the server-rendered store page
// and the leaderboard query that stickermule.com/stats itself issues.
package sm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	BaseURL    = "https://www.stickermule.com"
	graphqlURL = BaseURL + "/bridge/backend/graphql"
	userAgent  = "store-doctor/0.1 (+https://github.com/Anirudh-Nadipally/store-doctor)"
)

// Snapshot is everything Store Doctor knows about a store, read from its public page.
type Snapshot struct {
	Handle       string    `json:"handle"`
	Name         string    `json:"name"`
	Bio          string    `json:"bio"`
	HasAvatar    bool      `json:"hasAvatar"`
	HasBanner    bool      `json:"hasBanner"`
	Verified     bool      `json:"verified"`
	Pro          bool      `json:"pro"`
	Teams        int       `json:"teams"`
	JoinDate     time.Time `json:"joinDate"`
	Hidden       bool      `json:"hidden"`
	ItemCount    int       `json:"itemCount"`    // capped at PageItemLimit by the page itself
	ProductTypes int       `json:"productTypes"` // distinct product kinds among those items
	Orders       int       `json:"orders"`       // shown publicly on the page; used only as the eval label
	FetchedAt    time.Time `json:"fetchedAt"`
}

// PageItemLimit is how many items the store page renders server-side.
const PageItemLimit = 20

var ErrNotFound = errors.New("store not found")

type Client struct {
	HTTP     *http.Client
	Base     string
	MinDelay time.Duration // politeness: minimum gap between requests

	mu   sync.Mutex
	last time.Time
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}, Base: BaseURL, MinDelay: 700 * time.Millisecond}
}

func (c *Client) wait() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.MinDelay - time.Since(c.last); d > 0 {
		time.Sleep(d)
	}
	c.last = time.Now()
}

var handleRe = regexp.MustCompile(`^[A-Za-z0-9_\-]{2,64}$`)

// NormalizeHandle accepts "@name", "name" or a full store URL.
func NormalizeHandle(in string) (string, error) {
	h := strings.TrimSpace(in)
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimPrefix(h, "stickermule.com/")
	h = strings.TrimPrefix(h, "@")
	h = strings.Trim(h, "/")
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if !handleRe.MatchString(h) {
		return "", fmt.Errorf("not a valid store handle: %q", in)
	}
	return strings.ToLower(h), nil
}

func (c *Client) Fetch(ctx context.Context, handle string) (*Snapshot, error) {
	c.wait()
	req, _ := http.NewRequestWithContext(ctx, "GET", c.Base+"/"+handle, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("store page returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return ParseStorePage(handle, body)
}

var nextDataRe = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

// ParseStorePage extracts a Snapshot from the Apollo cache embedded in the page.
func ParseStorePage(handle string, html []byte) (*Snapshot, error) {
	m := nextDataRe.FindSubmatch(html)
	if m == nil {
		return nil, errors.New("page has no __NEXT_DATA__ block")
	}
	var doc struct {
		Props struct {
			ApolloState map[string]json.RawMessage `json:"apolloState"`
		} `json:"props"`
	}
	if err := json.Unmarshal(m[1], &doc); err != nil {
		return nil, fmt.Errorf("decode page data: %w", err)
	}
	st := doc.Props.ApolloState
	profKey := fmt.Sprintf(`UserProfile:{"vanityName":"%s"}`, handle)
	raw, ok := st[profKey]
	if !ok {
		// handles are case-insensitive on the site; fall back to any profile entry
		for k, v := range st {
			if strings.HasPrefix(k, "UserProfile:") {
				raw, ok = v, true
				break
			}
		}
	}
	if !ok {
		return nil, ErrNotFound
	}
	var p struct {
		Name            string            `json:"name"`
		VanityName      string            `json:"vanityName"`
		Avatar          *string           `json:"avatar"`
		BackgroundImage *string           `json:"backgroundImage"`
		JoinDate        string            `json:"joinDate"`
		Bio             *string           `json:"bio"`
		IsVerified      bool              `json:"isIdentityVerified"`
		IsPro           bool              `json:"isProUser"`
		Teams           []json.RawMessage `json:"teams"`
		Store           *struct {
			IsPrivate bool `json:"isPrivate"`
			IsHidden  bool `json:"isHidden"`
		} `json:"store"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	s := &Snapshot{Handle: handle, Name: p.Name, FetchedAt: time.Now().UTC(), Teams: len(p.Teams)}
	if p.VanityName != "" {
		s.Handle = strings.ToLower(p.VanityName)
	}
	s.HasAvatar = p.Avatar != nil && *p.Avatar != ""
	s.HasBanner = p.BackgroundImage != nil && *p.BackgroundImage != ""
	if p.Bio != nil {
		s.Bio = strings.TrimSpace(*p.Bio)
	}
	s.Verified, s.Pro = p.IsVerified, p.IsPro
	if p.Store != nil {
		s.Hidden = p.Store.IsHidden || p.Store.IsPrivate
	}
	if t, err := time.Parse(time.RFC3339, p.JoinDate); err == nil {
		s.JoinDate = t
	}

	// Items: the store entry lists item refs; each ref names its product kind.
	types := map[string]bool{}
	for k, v := range st {
		if !strings.HasPrefix(k, "Store:") {
			continue
		}
		var store map[string]json.RawMessage
		if json.Unmarshal(v, &store) != nil {
			continue
		}
		best := 0
		for field, val := range store {
			if !strings.HasPrefix(field, "storeItems(") {
				continue
			}
			var items struct {
				Items []struct {
					Ref string `json:"__ref"`
				} `json:"items"`
			}
			if json.Unmarshal(val, &items) != nil || len(items.Items) <= best {
				continue
			}
			best = len(items.Items)
			types = map[string]bool{}
			for _, it := range items.Items {
				var item struct {
					Product struct {
						ID string `json:"id"`
					} `json:"product"`
				}
				if json.Unmarshal(st[it.Ref], &item) == nil && item.Product.ID != "" {
					types[item.Product.ID] = true
				}
			}
		}
		s.ItemCount = best
	}
	s.ProductTypes = len(types)

	// Public order count, as shown on the page ("956 orders").
	if rq, ok := st["ROOT_QUERY"]; ok {
		var root map[string]json.RawMessage
		if json.Unmarshal(rq, &root) == nil {
			var sales map[string]json.RawMessage
			if json.Unmarshal(root["storeDashboardSales"], &sales) == nil {
				for k, v := range sales {
					if strings.HasPrefix(k, "total(") {
						_ = json.Unmarshal(v, &s.Orders)
					}
				}
			}
		}
	}
	return s, nil
}

// LeaderboardEntry is one row of the public store leaderboard.
type LeaderboardEntry struct {
	Handle string `json:"vanityName"`
	Orders int    `json:"salesCount"`
}

// Leaderboard pages through the public sales leaderboard (the same query
// stickermule.com/stats runs to render its table).
func (c *Client) Leaderboard(ctx context.Context, offset, limit int) ([]LeaderboardEntry, error) {
	c.wait()
	q := fmt.Sprintf(`{ publicProfiles(limit:%d, offset:%d, sortField:SALES){ users { vanityName salesCount } } }`, limit, offset)
	payload, _ := json.Marshal(map[string]string{"query": q})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.Base+"/bridge/backend/graphql", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			PublicProfiles struct {
				Users []LeaderboardEntry `json:"users"`
			} `json:"publicProfiles"`
		} `json:"data"`
		Errors []struct{ Message string } `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, errors.New(out.Errors[0].Message)
	}
	return out.Data.PublicProfiles.Users, nil
}

var _ = graphqlURL
