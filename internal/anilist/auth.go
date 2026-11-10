package anilist

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const OAuthBase = "https://anilist.co/api/v2/oauth"

func WithOAuthBase(base string) Option {
	return func(c *Client) { c.oauthBase = base }
}

const YoruClientID = "55278"

func (c *Client) AuthorizeTokenURL(clientID string) string {
	return c.oauthBase + "/authorize?" + url.Values{"client_id": {clientID}, "response_type": {"token"}}.Encode()
}

func PastedToken(pasted string) (Token, bool) {
	raw := strings.TrimSpace(pasted)
	if _, fragment, ok := strings.Cut(raw, "#"); ok {
		if v, err := url.ParseQuery(fragment); err == nil {
			raw = v.Get("access_token")
		}
	}
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return Token{}, false
	}
	token := Token{AccessToken: raw, TokenType: "Bearer", ExpiresIn: 365 * 24 * 3600}
	if parts := strings.Split(raw, "."); len(parts) == 3 {
		var claims struct {
			Exp int64 `json:"exp"`
		}
		body, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil && json.Unmarshal(body, &claims) == nil && claims.Exp > 0 {
			token.ExpiresIn = int(claims.Exp - time.Now().Unix())
		}
	}
	return token, true
}

func (c *Client) AuthorizeURL(clientID, redirectURI, state string) string {
	q := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"state":         {state},
	}
	return c.oauthBase + "/authorize?" + q.Encode()
}

type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (t Token) ExpiresAt() time.Time {
	return time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
}

func (c *Client) Exchange(ctx context.Context, clientID, clientSecret, redirectURI, code string) (Token, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"redirect_uri":  redirectURI,
		"code":          code,
	})
	if err != nil {
		return Token{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.oauthBase+"/token", bytes.NewReader(body))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Referer", Referer)

	res, err := c.http.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return Token{}, err
	}

	if res.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("anilist token exchange (HTTP %d): %s", res.StatusCode, exchangeError(raw))
	}

	var token Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return Token{}, fmt.Errorf("anilist token exchange: %w: %s", err, snippet(raw))
	}
	if token.AccessToken == "" {
		return Token{}, fmt.Errorf("anilist token exchange: empty access_token")
	}
	return token, nil
}

func exchangeError(raw []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Hint        string `json:"hint"`
		Message     string `json:"message"`
	}
	if json.Unmarshal(raw, &e) == nil {
		if said := strings.Join(slices.DeleteFunc([]string{e.Error, e.Description, e.Hint, e.Message},
			func(s string) bool { return s == "" }), ": "); said != "" {
			return said
		}
	}
	return snippet(raw)
}

func snippet(raw []byte) string {
	s := strings.Join(strings.Fields(string(raw)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

const viewerQuery = `query { Viewer { id name avatar { medium } mediaListOptions { scoreFormat } } }`

type Viewer struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Avatar struct {
		Medium string `json:"medium"`
	} `json:"avatar"`
	MediaListOptions struct {
		ScoreFormat string `json:"scoreFormat"`
	} `json:"mediaListOptions"`
}

func (c *Client) Viewer(ctx context.Context) (Viewer, error) {
	var out struct {
		Viewer Viewer `json:"Viewer"`
	}
	err := c.Query(ownAnswer(ctx), viewerQuery, nil, &out)
	return out.Viewer, err
}

func (c *Client) ViewerWith(ctx context.Context, token string) (Viewer, error) {
	return c.Viewer(context.WithValue(ctx, tokenKey{}, token))
}
