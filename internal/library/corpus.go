package library

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/corpus"
	"yoru/internal/store"
)

const (
	// AniList accepts 50 ids per request.
	batchSize = 50

	seedInterval     = 30 * 24 * time.Hour
	discoverInterval = 24 * time.Hour
)

type Ingester struct {
	store   *store.Store
	fetcher *corpus.Fetcher
	al      *anilist.Client
	log     *slog.Logger

	// Relations, when set, links a just-discovered anime to the saved shows it continues.
	Relations *Relations
}

func NewIngester(s *store.Store, f *corpus.Fetcher, al *anilist.Client, log *slog.Logger) *Ingester {
	return &Ingester{store: s, fetcher: f, al: al, log: log}
}

type Report struct {
	Seeded     int `json:"seeded"`
	Discovered int `json:"discovered"`
	Fetched    int `json:"fetched"`
	Titles     int `json:"titles"`
	Dead       int `json:"dead"`
	Linked     int `json:"linked"`
}

func (i *Ingester) Run(ctx context.Context, force bool) (Report, error) {
	var rep Report
	var seedErr error

	// A failed seed must not also cost the day's new-season discovery.
	if due, _ := i.due(ctx, "manami", seedInterval, force); due {
		n, err := i.seed(ctx)
		rep.Seeded = n
		if err != nil {
			seedErr = fmt.Errorf("seed: %w", err)
		}
	}

	if due, _ := i.due(ctx, "animeapi", discoverInterval, force); due {
		// The first discovery finds every sequel ever made, not news: nothing is announced from it.
		before, _ := i.store.SourceRefreshedAt(ctx, "animeapi")
		missing, upstream, err := i.discover(ctx)
		if err != nil {
			return rep, errors.Join(seedErr, fmt.Errorf("discover: %w", err))
		}
		rep.Discovered = len(missing)

		var recent []int
		rep.Fetched, rep.Titles, rep.Dead, recent, err = i.fetchTitles(ctx, missing)
		if err != nil {
			return rep, errors.Join(seedErr, fmt.Errorf("fetch titles: %w", err))
		}
		rep.Linked = i.linkNew(anilist.Background(ctx), recent, !before.IsZero())
		// Marked only once fetched, so a retry picks up the rest.
		if err := i.store.MarkSource(ctx, "animeapi", upstream); err != nil {
			return rep, errors.Join(seedErr, err)
		}
	}
	return rep, seedErr
}

func (i *Ingester) due(ctx context.Context, name string, every time.Duration, force bool) (bool, error) {
	if force {
		return true, nil
	}
	at, err := i.store.SourceRefreshedAt(ctx, name)
	if err != nil {
		return true, err
	}
	return at.IsZero() || time.Since(at) > every, nil
}

// seed loads the frozen manami database; it never changes, so this runs once.
func (i *Ingester) seed(ctx context.Context) (int, error) {
	i.log.Info("seeding title corpus")

	batch := make([]store.CorpusEntry, 0, 2000)
	var total, titles int

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := i.store.SaveCorpus(ctx, batch)
		if err != nil {
			return err
		}
		titles += n
		batch = batch[:0]
		return nil
	}

	count, err := i.fetcher.Manami(ctx, func(e corpus.Entry) error {
		batch = append(batch, toStoreEntry(e))
		total++
		if len(batch) >= 2000 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return total, err
	}
	if err := flush(); err != nil {
		return total, err
	}

	i.log.Info("corpus seeded", "anime", total, "titles", titles)
	return total, i.store.MarkSource(ctx, "manami", count)
}

// discover diffs a daily-rebuilt id list against stored ids to notice new seasons.
func (i *Ingester) discover(ctx context.Context) ([]int, int, error) {
	live, err := i.fetcher.AnimeAPI(ctx)
	if err != nil {
		return nil, 0, err
	}
	known, err := i.store.KnownIDs(ctx)
	if err != nil {
		return nil, 0, err
	}

	missing := make([]int, 0, 512)
	for id := range live {
		if _, seen := known[id]; !seen {
			missing = append(missing, id)
		}
	}

	i.log.Info("id discovery", "upstream", len(live), "known", len(known), "new", len(missing))
	return missing, len(live), nil
}

// Ids that return nothing are recorded so they are never requested again. recent: the new ones airing,
// announced or from the last two years, which may continue a show already saved.
func (i *Ingester) fetchTitles(ctx context.Context, ids []int) (fetched, titles, dead int, recent []int, err error) {
	thisYear := time.Now().Year()
	for start := 0; start < len(ids); start += batchSize {
		chunk := ids[start:min(start+batchSize, len(ids))]

		media, err := i.al.MediaByIDs(ctx, chunk)
		incomplete := errors.Is(err, anilist.ErrIncomplete)
		if err != nil && !incomplete {
			return fetched, titles, dead, recent, err
		}

		entries := make([]store.CorpusEntry, 0, len(media))
		found := make(map[int]struct{}, len(media))
		for _, m := range media {
			found[m.ID] = struct{}{}
			entries = append(entries, mediaToEntry(m))
			airing := m.Status != nil && (*m.Status == "RELEASING" || *m.Status == "NOT_YET_RELEASED")
			if airing || (m.StartDate.Year != nil && *m.StartDate.Year >= thisYear-1) {
				recent = append(recent, m.ID)
			}
		}

		n, err := i.store.SaveCorpus(ctx, entries)
		if err != nil {
			return fetched, titles, dead, recent, err
		}
		fetched += len(entries)
		titles += n

		var gone []int
		for _, id := range chunk {
			if _, ok := found[id]; !ok && !incomplete {
				gone = append(gone, id)
			}
		}
		if err := i.store.MarkDead(ctx, gone); err != nil {
			return fetched, titles, dead, recent, err
		}
		dead += len(gone)
	}
	return fetched, titles, dead, recent, nil
}

// linkNew walks the franchise of each new anime that relates to a saved show, so a just-announced season,
// film or OVA joins that show's Seasons or Related the day it is found, not at the show's weekly walk.
func (i *Ingester) linkNew(ctx context.Context, ids []int, announce bool) int {
	// A first discovery can be thousands; past these, the weekly walk on opening catches up. Announcing reads
	// every new one (a day's few; a first discovery announces nothing): a sequel missed here is never told.
	const maxIDs, maxWalks, maxAnnounce = 100, 20, 1000
	if i.Relations == nil || len(ids) == 0 {
		return 0
	}
	// Newest first: ids grow, so a cut keeps this week's announcements over older finds.
	ids = slices.Clone(ids)
	slices.SortFunc(ids, func(a, b int) int { return b - a })
	limit := maxIDs
	if announce {
		limit = maxAnnounce
	}
	ids = ids[:min(len(ids), limit)]

	walked, failed := 0, 0
	for start := 0; start < len(ids) && (announce || walked < maxWalks); start += 25 {
		media, err := i.al.Relations(ctx, ids[start:min(start+25, len(ids))])
		if err != nil {
			i.log.Warn("relations of new anime", "err", err)
			// One failure may pass, so the next batch is tried; two in a row is AniList down or limiting.
			if failed++; failed >= 2 {
				return walked
			}
			continue
		}
		failed = 0
		for _, m := range media {
			if announce {
				i.announceSeason(ctx, m)
			}
			if walked >= maxWalks || start >= maxIDs || !i.continuesSaved(ctx, m) {
				continue
			}
			// Reached by an earlier walk this run (a sequel's film or OVA): its franchise is already current.
			if known, _ := i.store.HasRelations(ctx, m.ID); known && !i.store.RelationsStale(ctx, m.ID, relationsMaxAge) {
				continue
			}
			n, err := i.Relations.Fetch(ctx, m.ID)
			if err != nil {
				i.log.Warn("link new anime", "anime", m.ID, "err", err)
				continue
			}
			if n > 0 {
				walked++
			}
		}
	}
	if walked > 0 {
		i.log.Info("new anime linked to saved shows", "count", walked)
	}
	return walked
}

// Statuses whose shows' sequels are news: anything on the list but dropped.
var seasonNews = map[string]bool{"CURRENT": true, "REPEATING": true, "COMPLETED": true, "PAUSED": true, "PLANNING": true}

// announceSeason tells of a new anime continuing a show on the list (its PREQUEL edge), a single time.
func (i *Ingester) announceSeason(ctx context.Context, m anilist.RelatedMedia) {
	for _, e := range m.Relations.Edges {
		if e.Type != "PREQUEL" || e.Node.Type != "ANIME" {
			continue
		}
		entry, err := i.store.ListEntry(ctx, e.Node.ID)
		if err != nil || !seasonNews[entry.Status] || i.store.Notified(ctx, store.NotifySeason, m.ID) {
			continue
		}
		rec, _ := i.store.CorpusRecord(ctx, m.ID)
		title := cmp.Or(i.store.ShownTitle(ctx, m.ID), rec.Primary)
		if title == "" {
			return
		}
		body := "Follows " + cmp.Or(i.store.ShownTitle(ctx, e.Node.ID), "a show on your list")
		if when := monthYear(m.StartDate); when != "" {
			body += " · from " + when
		}
		id := m.ID
		if _, err := i.store.AddNotification(ctx, store.Notification{
			Kind: store.NotifySeason, AnimeID: &id, Title: title + " is on AniList", Body: body,
			Payload: map[string]any{"follows": e.Node.ID},
		}); err != nil {
			i.log.Warn("announce new season", "anime", m.ID, "err", err)
		}
		return
	}
}

// monthYear is "Jan 2027", "2027", or "" for an undated announcement.
func monthYear(d anilist.FuzzyDate) string {
	switch {
	case d.Year == nil:
		return ""
	case d.Month == nil || *d.Month < 1 || *d.Month > 12:
		return strconv.Itoa(*d.Year)
	}
	return time.Month(*d.Month).String()[:3] + " " + strconv.Itoa(*d.Year)
}

// continuesSaved: one of the new anime's season or related edges reaches a show Yoru keeps.
func (i *Ingester) continuesSaved(ctx context.Context, m anilist.RelatedMedia) bool {
	for _, e := range m.Relations.Edges {
		if e.Node.Type == "ANIME" && relatedEdges[e.Type] && i.store.IsSaved(ctx, e.Node.ID) {
			return true
		}
	}
	return false
}

func toStoreEntry(e corpus.Entry) store.CorpusEntry {
	out := store.CorpusEntry{
		AniListID: e.AniListID, MalID: e.MalID, AniDBID: e.AniDBID,
		Kind: e.Kind, Episodes: e.Episodes, Year: e.Year, Season: e.Season,
	}
	for _, t := range e.Titles {
		out.Titles = append(out.Titles, store.CorpusTitle{Text: t.Text, Kind: t.Kind, Lang: t.Lang})
	}
	return out
}

func mediaToEntry(m anilist.Media) store.CorpusEntry {
	out := store.CorpusEntry{AniListID: m.ID}
	if m.IDMal != nil {
		out.MalID = *m.IDMal
	}
	if m.Format != nil {
		out.Kind = *m.Format
	}
	if m.Episodes != nil {
		out.Episodes = *m.Episodes
	}
	if m.SeasonYear != nil {
		out.Year = *m.SeasonYear
	}
	if m.Season != nil {
		out.Season = *m.Season
	}

	add := func(text, kind string) {
		if text != "" {
			out.Titles = append(out.Titles, store.CorpusTitle{Text: text, Kind: kind})
		}
	}
	if m.Title.Romaji != nil {
		add(*m.Title.Romaji, "primary")
	}
	if m.Title.English != nil {
		add(*m.Title.English, "official")
	}
	if m.Title.Native != nil {
		add(*m.Title.Native, "native")
	}
	for _, s := range m.Synonyms {
		add(s, "synonym")
	}
	return out
}
