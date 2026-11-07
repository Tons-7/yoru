package library

import (
	"context"
	"errors"
	"strings"

	"yoru/internal/store"
)

var errNotReadied = errors.New("next episode not readied")

func (p *Playback) ReadyNext(ctx context.Context, animeID, episode int) (store.Pick, error) {
	if p == nil || p.torrent == nil || p.prefetch == nil {
		return store.Pick{}, errNotReadied
	}
	picks, _, err := p.store.PlayOrder(ctx, animeID, episode, 1)
	if err != nil || len(picks) == 0 {
		return store.Pick{}, errNotReadied
	}
	next := picks[0]
	if _, ok := localCopy(ctx, p.store, p.log, next.AnimeID, next.Episode); ok {
		return next, nil
	}

	rec, held, err := p.store.TorrentForEpisode(ctx, next.AnimeID, epKey(next.Episode))
	if err != nil {
		return next, err
	}
	if !held {
		if rec, held = p.prefetch.Fetching(next.AnimeID, next.Episode); !held {
			prefs, err := p.store.Prefs(ctx, next.AnimeID)
			if err != nil || !prefs.Bool("playback.autonext") {
				return next, errNotReadied
			}
			if _, _, err := p.prefetch.fetch(ctx, next.AnimeID, next.Episode, 0, Preferences(prefs), false); err != nil {
				return next, err
			}
			if rec, held, err = p.store.TorrentForEpisode(ctx, next.AnimeID, epKey(next.Episode)); err != nil || !held {
				return next, errNotReadied
			}
		}
	}
	if p.prefetch.pausedByUser(rec.InfoHash) || p.prefetch.pausedByUser(FileKey(rec.InfoHash, rec.FileIndex)) {
		return next, errNotReadied
	}

	live, err := p.torrent.Live(ctx)
	if err != nil {
		return next, err
	}
	id, ok := live[strings.ToLower(rec.InfoHash)]
	if !ok {
		return next, errNotReadied
	}
	if err := p.torrent.Start(ctx, id); err != nil {
		p.log.Debug("ready next: start", "torrent", id, "err", err)
	}
	return next, p.torrent.Prewarm(ctx, id, rec.FileIndex, 0)
}
