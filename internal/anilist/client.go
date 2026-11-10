package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	Endpoint = "https://graphql.anilist.co"

	Referer = "https://anilist.co/"

	requestsPerMinute = 30
	burst             = 5
	maxRetries        = 3
)

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
	log     *slog.Logger

	tokenMu   sync.RWMutex
	token     string
	endpoint  string
	oauthBase string

	cache  *responseCache
	totals totals

	mu          sync.Mutex
	pausedUntil time.Time
	downUntil   time.Time
	archive     Archive

	refusedToken string
	refusedAt    time.Time
}

const refusedFor = 10 * time.Minute

func (c *Client) noteRefused(token string) {
	c.mu.Lock()
	c.refusedToken, c.refusedAt = token, time.Now()
	c.mu.Unlock()
}

func (c *Client) loginRefused(token string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return token != "" && token == c.refusedToken && time.Since(c.refusedAt) < refusedFor
}

type Option func(*Client)

func WithEndpoint(url string) Option {
	return func(c *Client) { c.endpoint = url }
}

func WithoutRateLimit() Option {
	return func(c *Client) { c.limiter = rate.NewLimiter(rate.Inf, 1) }
}

func New(log *slog.Logger, opts ...Option) *Client {
	c := &Client{
		http:      &http.Client{Timeout: 30 * time.Second},
		limiter:   rate.NewLimiter(rate.Every(time.Minute/requestsPerMinute), burst),
		log:       log,
		endpoint:  Endpoint,
		oauthBase: OAuthBase,
		cache:     newResponseCache(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) SetToken(token string) {
	c.tokenMu.Lock()
	c.token = token
	c.tokenMu.Unlock()
}

func (c *Client) bearer() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.token
}

func (c *Client) Authenticated() bool { return c.bearer() != "" }

type anonymousKey struct{}

type tokenKey struct{}

func (c *Client) bearerFor(ctx context.Context) string {
	if ctx.Value(anonymousKey{}) != nil {
		return ""
	}
	if token, ok := ctx.Value(tokenKey{}).(string); ok {
		return token
	}
	return c.bearer()
}

type GraphQLError struct {
	Message    string              `json:"message"`
	Status     int                 `json:"status"`
	Validation map[string][]string `json:"validation"`
}

type Errors []GraphQLError

func (e Errors) Error() string {
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Message
	}
	return "anilist: " + strings.Join(msgs, "; ")
}

func (e Errors) Unauthorized() bool {
	for _, err := range e {
		if err.Status == 401 || (err.Status == 400 && strings.Contains(err.Message, "Invalid token")) {
			return true
		}
	}
	return false
}

type response struct {
	Data   json.RawMessage `json:"data"`
	Errors Errors          `json:"errors"`
}

func (c *Client) queryAnonymous(ctx context.Context, query string, vars map[string]any, out any) error {
	if c.bearer() == "" {
		return errors.New("anilist: already anonymous")
	}

	return c.Query(context.WithValue(ctx, anonymousKey{}, true), query, vars, out)
}

func (c *Client) Query(ctx context.Context, query string, vars map[string]any, out any) error {

	token := c.bearer()
	public := token != "" && !isMutation(query) && ctx.Value(anonymousKey{}) == nil && ctx.Value(tokenKey{}) == nil
	triedAnonymous := false
	if public && c.loginRefused(token) {
		if c.queryAnonymous(ctx, query, vars, out) == nil {
			return nil
		}

		triedAnonymous = true
	}
	err := c.query(ctx, query, vars, out)
	if errs, ok := errors.AsType[Errors](err); ok && errs.Unauthorized() && public {
		c.noteRefused(token)
		if !triedAnonymous && c.queryAnonymous(ctx, query, vars, out) == nil {
			return nil
		}
	}
	return err
}

func (c *Client) query(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}

	if c.cache == nil || isMutation(query) {
		return c.execute(ctx, query, body, out)
	}

	key := cacheKey(query, body, c.bearerFor(ctx))
	if data, ok := c.cache.get(key); ok {
		servedFrom(ctx).noteLive()
		return decodeInto(data, out)
	}

	saved := archiveKey(body, c.bearerFor(ctx) != "")

	if c.isDown() && c.fromArchive(ctx, saved, out) {
		return nil
	}
	if c.savedFirst(ctx, query, body, key, saved, out) {
		return nil
	}

	wait, mine := c.cache.enter(key)
	if !mine {
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		if data, ok := c.cache.get(key); ok {
			servedFrom(ctx).noteLive()
			return decodeInto(data, out)
		}
		if c.isDown() && c.fromArchive(ctx, saved, out) {
			return nil
		}

		return c.fetch(ctx, query, body, key, saved, out)
	}
	defer c.cache.done(key, wait)
	return c.fetch(ctx, query, body, key, saved, out)
}

const refreshWait = 2 * time.Minute

type liveFirstKey struct{}

func LiveFirst(ctx context.Context) context.Context {
	return context.WithValue(ctx, liveFirstKey{}, true)
}

func (c *Client) savedFirst(ctx context.Context, query string, body []byte, key, saved string, out any) bool {
	served, archive := servedFrom(ctx), c.archiver()
	if served == nil || archive == nil || !keepsAnswers(ctx) || ctx.Value(liveFirstKey{}) != nil || c.cache.refused(key) {
		return false
	}
	data, at, ok := archive.LoadAnswer(ctx, saved)
	if !ok || decodeInto(data, out) != nil {
		return false
	}
	if expires := at.Add(cacheTTL); time.Now().Before(expires) {
		c.cache.putUntil(key, data, expires)
		served.noteLive()
		return true
	}
	served.noteRefreshing()
	wait, mine := c.cache.enter(key)
	if !mine {
		return true
	}
	behind, cancel := context.WithTimeout(Background(context.WithoutCancel(ctx)), refreshWait)
	go func() {
		defer cancel()
		defer c.cache.done(key, wait)
		var fresh json.RawMessage
		if err := c.execute(behind, query, body, &fresh); err != nil {
			if unreachable(behind, err) {
				c.markDown()
			} else {
				c.cache.noteFailed(key)
			}
			c.log.Debug("refresh a saved anilist answer", "err", err)
			return
		}
		c.cache.put(key, fresh)
		if err := archive.SaveAnswer(behind, saved, fresh); err != nil {
			c.log.Debug("save anilist answer", "err", err)
		}
	}()
	return true
}

func (c *Client) fetch(ctx context.Context, query string, body []byte, key, saved string, out any) error {
	var data json.RawMessage
	if err := c.execute(ctx, query, body, &data); err != nil {
		if !unreachable(ctx, err) {
			return err
		}
		c.markDown()
		if c.fromArchive(ctx, saved, out) {
			c.log.Warn("anilist unreachable, answering from the saved copy", "err", err)
			return nil
		}
		return err
	}
	c.cache.put(key, data)

	if served := servedFrom(ctx); served != nil {
		served.noteLive()
		if archive := c.archiver(); archive != nil && keepsAnswers(ctx) {
			go func() {
				if err := archive.SaveAnswer(context.WithoutCancel(ctx), saved, data); err != nil {
					c.log.Debug("save anilist answer", "err", err)
				}
			}()
		}
	}
	return decodeInto(data, out)
}

func (c *Client) QueryFresh(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	return c.execute(ctx, query, body, out)
}

func decodeInto(data []byte, out any) error {
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *Client) execute(ctx context.Context, query string, body []byte, out any) error {
	return c.run(ctx, body, out, true)
}

func (c *Client) MutateOnce(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	return c.run(ctx, body, out, false)
}

type backgroundKey struct{}

func Background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

const yieldMax = 30 * time.Second

func (c *Client) yield(ctx context.Context) error {
	if ctx.Value(backgroundKey{}) == nil || c.limiter.Limit() == rate.Inf {
		return nil
	}
	deadline := time.Now().Add(yieldMax)
	for c.limiter.Tokens() < countReserve && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

func (c *Client) run(ctx context.Context, body []byte, out any, repeatable bool) error {
	for attempt := 0; ; attempt++ {
		if err := c.yield(ctx); err != nil {
			return err
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		if err := c.awaitPause(ctx); err != nil {
			return err
		}

		res, raw, err := c.post(ctx, body)
		if err != nil {
			if repeatable && attempt < maxRetries {
				c.sleep(ctx, backoff(attempt))
				continue
			}
			return err
		}

		switch {
		case res.StatusCode == http.StatusTooManyRequests:
			wait := retryAfter(res)
			c.pauseFor(wait)
			if attempt >= maxRetries {
				return fmt.Errorf("anilist: rate limited after %d attempts", attempt)
			}
			c.log.Warn("anilist rate limited", "wait", wait)
			continue

		case res.StatusCode == http.StatusForbidden || res.StatusCode >= 500:
			if (repeatable || res.StatusCode == http.StatusForbidden) && attempt < maxRetries {
				c.sleep(ctx, backoff(attempt))
				continue
			}
		}

		if remaining(res) == 0 {
			c.log.Debug("anilist quota exhausted, pausing subsequent requests")
			c.pauseFor(time.Minute)
		}

		var parsed response
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return fmt.Errorf("anilist: decode response (HTTP %d): %w", res.StatusCode, err)
		}

		if len(parsed.Errors) > 0 {
			return parsed.Errors
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(parsed.Data, out)
	}
}

func (c *Client) post(ctx context.Context, body []byte) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	req.Header.Set("Referer", Referer)
	if token := c.bearerFor(ctx); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	return res, raw, err
}

func (c *Client) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (c *Client) pauseFor(d time.Duration) {
	until := time.Now().Add(d)
	c.mu.Lock()
	defer c.mu.Unlock()
	if until.After(c.pausedUntil) {
		c.pausedUntil = until
	}
}

func (c *Client) awaitPause(ctx context.Context) error {
	c.mu.Lock()
	wait := time.Until(c.pausedUntil)
	c.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

func retryAfter(res *http.Response) time.Duration {
	if v, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && v > 0 {
		return time.Duration(v)*time.Second + 500*time.Millisecond
	}
	if v, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if d := time.Until(time.Unix(v, 0)); d > 0 {
			return d + 500*time.Millisecond
		}
	}
	return time.Minute
}

func remaining(res *http.Response) int {
	v, err := strconv.Atoi(res.Header.Get("X-RateLimit-Remaining"))
	if err != nil {
		return -1
	}
	return v
}

func backoff(attempt int) time.Duration {
	return time.Duration(1<<attempt) * time.Second
}

func (e Errors) NotFound() bool {
	for _, err := range e {
		if err.Status == 404 {
			return true
		}
	}
	return false
}
