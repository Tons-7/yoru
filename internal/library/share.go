package library

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"yoru/internal/player"
)

// errNotSharing: skips.share is off, or AniSkip can't place the episode; nothing is sent.
var errNotSharing = errors.New("not sharing skip times")

// ShareSkip gives AniSkip a time it lacks (from chapters or the viewer's mark), once per episode and part.
func (e *Enricher) ShareSkip(ctx context.Context, animeID, episode int, r player.SkipRange, length float64) error {
	part := r.Part()
	if part != "op" && part != "ed" || length <= 0 || !e.sharing(ctx) {
		return errNotSharing
	}
	malID, err := e.store.MalID(ctx, animeID)
	if err != nil || malID == 0 {
		return errNotSharing
	}
	key := fmt.Sprintf("submit:%d:%d:%s", malID, episode, part)
	if first, err := e.store.MarkShared(ctx, key); err != nil || !first {
		return err
	}
	if err := e.meta.SubmitSkip(ctx, malID, episode, part, r.Start, r.End, length, e.submitter(ctx)); err != nil {
		e.store.UnmarkShared(ctx, key)
		return err
	}
	e.log.Info("skip time shared with AniSkip", "anime", animeID, "episode", episode, "part", part)
	return nil
}

// VoteSkip tells AniSkip a time was right or wrong, each verdict once: a "wrong" after a kept skip still goes.
func (e *Enricher) VoteSkip(ctx context.Context, skipID string, up bool) error {
	if skipID == "" || !e.sharing(ctx) {
		return errNotSharing
	}
	key := fmt.Sprintf("vote:%s:%t", skipID, up)
	if first, err := e.store.MarkShared(ctx, key); err != nil || !first {
		return err
	}
	if err := e.meta.VoteSkip(ctx, skipID, up); err != nil {
		e.store.UnmarkShared(ctx, key)
		return err
	}
	return nil
}

func (e *Enricher) sharing(ctx context.Context) bool {
	prefs, err := e.store.Prefs(ctx, 0)
	return err == nil && prefs.Bool("skips.share")
}

// submitter is the anonymous id AniSkip groups this install's times by, made on first use.
func (e *Enricher) submitter(ctx context.Context) string {
	if id, err := e.store.Setting(ctx, "skips.submitter"); err == nil && id != "" {
		return id
	}
	b := make([]byte, 16)
	rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	e.store.SetSetting(ctx, "skips.submitter", id)
	return id
}
