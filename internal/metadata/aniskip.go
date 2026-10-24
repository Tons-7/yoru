package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// AniSkipBase is v2's skip-times root, for giving times back and voting on them.
const AniSkipBase = "https://api.aniskip.com/v2/skip-times"

// SubmitSkip offers AniSkip a time it lacks; kind is op or ed, submitter the anonymous id it groups ours by.
func (c *Client) SubmitSkip(ctx context.Context, malID, episode int, kind string, start, end, length float64, submitter string) error {
	return c.post(ctx, fmt.Sprintf("%s/%d/%d", c.url("aniskip-base", AniSkipBase), malID, episode), map[string]any{
		"skipType": kind, "providerName": "Yoru", "startTime": start, "endTime": end,
		"episodeLength": length, "submitterId": submitter,
	})
}

// VoteSkip says a time was right (up) or wrong.
func (c *Client) VoteSkip(ctx context.Context, skipID string, up bool) error {
	vote := "downvote"
	if up {
		vote = "upvote"
	}
	return c.post(ctx, c.url("aniskip-base", AniSkipBase)+"/vote/"+url.PathEscape(skipID), map[string]any{"voteType": vote})
}

func (c *Client) post(ctx context.Context, url string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "yoru/0.1")
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}
	return nil
}
