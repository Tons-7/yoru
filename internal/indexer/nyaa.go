package indexer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const nyaaDelay = 2 * time.Second

type Nyaa struct {
	http     *http.Client
	base     string
	name     string
	category string
	limiter  *rate.Limiter
	blocked  atomic.Int64
}

func (n *Nyaa) backedOff() error {
	if until := n.blocked.Load(); until > time.Now().UnixNano() {
		return fmt.Errorf("HTTP 429 (asked to wait %s)", time.Until(time.Unix(0, until)).Round(time.Second))
	}
	return nil
}

func (n *Nyaa) backOff(res *http.Response) {
	if res.StatusCode != http.StatusTooManyRequests && res.StatusCode != http.StatusServiceUnavailable {
		return
	}
	wait := time.Minute
	if s, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && s > 0 {
		wait = min(time.Duration(s)*time.Second, 10*time.Minute)
	}
	n.blocked.Store(time.Now().Add(wait).UnixNano())
}

type NyaaOption func(*Nyaa)

func WithNyaaDelay(d time.Duration) NyaaOption {
	return func(n *Nyaa) { n.limiter = rate.NewLimiter(rate.Every(d), 1) }
}

func NewNyaa(base string, opts ...NyaaOption) *Nyaa {
	n := &Nyaa{
		http:    &http.Client{Timeout: 30 * time.Second},
		base:    strings.TrimRight(base, "/"),
		limiter: rate.NewLimiter(rate.Every(nyaaDelay), 1),
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

func (n *Nyaa) Name() string {
	if n.name != "" {
		return n.name
	}
	return "nyaa"
}

func (n *Nyaa) defaultCategory() string {
	if n.category != "" {
		return n.category
	}
	return CategoryEnglishSubbed
}

const CategorySukebeiAnime = "1_1"

func NewSukebei(base string, opts ...NyaaOption) *Nyaa {
	adult := func(n *Nyaa) { n.name = "sukebei"; n.category = CategorySukebeiAnime }
	return NewNyaa(base, append([]NyaaOption{adult}, opts...)...)
}

func (n *Nyaa) Search(ctx context.Context, q Query) ([]Torrent, error) {
	if strings.TrimSpace(q.Text) == "" {
		return nil, errors.New("nothing to search for")
	}
	return n.searchHTML(ctx, q)
}

var sizeUnits = map[string]int64{
	"b": 1, "byte": 1, "bytes": 1, "kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40,
	"kb": 1000, "mb": 1000 * 1000, "gb": 1000 * 1000 * 1000, "tb": 1000 * 1000 * 1000 * 1000,
}

var sizeValue = regexp.MustCompile(`^([0-9][0-9,.]*)\s*([a-zA-Z]+)$`)

func ParseSize(s string) int64 {
	m := sizeValue.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil {
		return 0
	}
	unit, ok := sizeUnits[strings.ToLower(m[2])]
	if !ok {
		return 0
	}
	return int64(value * float64(unit))
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
