package library

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/store"
)

type Relations struct {
	store *store.Store
	al    *anilist.Client
	log   *slog.Logger
}

func NewRelations(s *store.Store, al *anilist.Client, log *slog.Logger) *Relations {
	return &Relations{store: s, al: al, log: log}
}

var seasonEdges = store.SeasonKinds

var relatedEdges = map[string]bool{
	"SIDE_STORY": true, "PARENT": true, "SPIN_OFF": true, "ALTERNATIVE": true,
	"SUMMARY": true, "PREQUEL": true, "SEQUEL": true,
}

const relationsMaxAge = 7 * 24 * time.Hour

func (r *Relations) Fetch(ctx context.Context, animeID int) (int, error) {
	const maxHops = 6

	seen := map[int]bool{animeID: true}
	frontier := []int{animeID}
	var saved []store.Relation
	var walked, fetched []int
	dates := map[int]string{}
	answered := false

	for hop := 0; hop < maxHops && len(frontier) > 0; hop++ {
		media, err := r.al.Relations(ctx, frontier)
		if err != nil {
			return 0, err
		}

		var next []int
		for _, m := range media {
			if m.ID == animeID {
				answered = true
			}
			walked, fetched = append(walked, m.ID), append(fetched, m.ID)
			if d := m.StartDate.String(); d != "" {
				dates[m.ID] = d
			}
			for _, edge := range m.Relations.Edges {
				if !relatedEdges[edge.Type] || edge.Node.Type != "ANIME" {
					continue
				}
				saved = append(saved, store.Relation{
					AnimeID: m.ID, RelatedID: edge.Node.ID, Kind: edge.Type,
				})
				if d := edge.Node.StartDate.String(); d != "" {
					dates[edge.Node.ID] = d
				}
				if !series(edge.Node.Format) {
					walked = append(walked, edge.Node.ID)
				}
				if seasonEdges[edge.Type] && !seen[edge.Node.ID] {
					seen[edge.Node.ID] = true
					next = append(next, edge.Node.ID)
				}
			}
		}
		frontier = next
	}

	if anilist.UsedSaved(ctx) {
		return 0, nil
	}
	if !answered {
		r.log.Warn("anilist did not answer for the show; its franchise stays unknown", "anime", animeID)
		return 0, nil
	}
	if err := r.store.MarkRelationsFetched(ctx, walked); err != nil {
		r.log.Warn("record relation fetch", "anime", animeID, "err", err)
	}

	if len(saved) == 0 {
		return 0, r.store.MarkNoRelations(ctx, animeID)
	}
	if err := r.store.SaveRelations(ctx, fetched, saved); err != nil {
		return 0, err
	}
	r.nameMembers(ctx, seen)

	groups, err := r.store.RebuildFranchisesFor(ctx, slices.Collect(maps.Keys(seen)), dates)
	if err != nil {
		return 0, err
	}
	r.log.Info("franchise resolved", "anime", animeID, "edges", len(saved), "groups", groups)
	return len(saved), nil
}

func (r *Relations) nameMembers(ctx context.Context, members map[int]bool) {
	const maxFetch = 25

	var missing []int
	for id := range members {
		names, err := r.store.SearchTitles(ctx, id)
		if err == nil && len(names) == 0 {
			missing = append(missing, id)
		}
	}
	if dead, err := r.store.DeadAnime(ctx, missing); err == nil {
		missing = slices.DeleteFunc(missing, func(id int) bool { return dead[id] })
	}
	if len(missing) == 0 {
		return
	}
	slices.Sort(missing)
	if len(missing) > maxFetch {
		missing = missing[:maxFetch]
	}

	if _, err := NewImporter(r.store, r.al, r.log).Hydrate(ctx, missing); err != nil {
		r.log.Warn("franchise titles", "ids", len(missing), "err", err)
	}
}

func (r *Relations) Ensure(ctx context.Context, animeID int) error {
	known, err := r.store.HasRelations(ctx, animeID)
	if err != nil {
		return err
	}
	if !known || r.store.RelationsStale(ctx, animeID, relationsMaxAge) {
		_, err = r.Fetch(ctx, animeID)
		return err
	}

	franchise, err := r.store.Franchise(ctx, animeID)
	if err != nil {
		return err
	}
	members := make(map[int]bool, len(franchise.Seasons))
	for _, s := range franchise.Seasons {
		members[s.ID] = true
	}
	r.nameMembers(ctx, members)
	return nil
}
