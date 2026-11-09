package library

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"sync"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/metadata"
	"yoru/internal/score"
	"yoru/internal/store"
)

type Enricher struct {
	store *store.Store
	meta  *metadata.Client
	ani   *anilist.Client
	log   *slog.Logger

	mu            sync.Mutex
	fetching      map[int]struct{}
	stilling      map[int]struct{}
	cast          map[int]castEntry
	extra         map[int]extraEntry
	people        map[int]cached[metadata.Person]
	characters    map[int]cached[metadata.CharacterPage]
	skipsAsked    map[[2]int]time.Time
	fillerShows   map[string]string
	fillerShowsAt time.Time
}

func NewEnricher(s *store.Store, m *metadata.Client, log *slog.Logger) *Enricher {
	return &Enricher{
		store: s, meta: m, log: log,
		fetching: map[int]struct{}{},
		stilling: map[int]struct{}{},
		cast:     map[int]castEntry{},
		extra:    map[int]extraEntry{},

		people:     map[int]cached[metadata.Person]{},
		characters: map[int]cached[metadata.CharacterPage]{},

		skipsAsked: map[[2]int]time.Time{},
	}
}

func (e *Enricher) WithAniList(c *anilist.Client) *Enricher {
	e.ani = c
	return e
}

func (e *Enricher) Anime(ctx context.Context, malID int) (metadata.AnimeDetail, error) {
	return e.meta.Anime(ctx, malID)
}

const castTTL = 12 * time.Hour

const partialTTL = 10 * time.Minute

const cacheShows = 200

func prune[T any](m map[int]T, at func(T) time.Time, ttl time.Duration) {
	for id, v := range m {
		if time.Since(at(v)) >= ttl {
			delete(m, id)
		}
	}
	for len(m) >= cacheShows {
		oldest, first := 0, true
		for id, v := range m {
			if first || at(v).Before(at(m[oldest])) {
				oldest, first = id, false
			}
		}
		delete(m, oldest)
	}
}

type cached[T any] struct {
	value T
	at    time.Time
}

func cachedFetch[T any](e *Enricher, m map[int]cached[T], id int, fetch func() (T, error)) (T, error) {
	e.mu.Lock()
	if hit, ok := m[id]; ok && time.Since(hit.at) < castTTL {
		e.mu.Unlock()
		return hit.value, nil
	}
	e.mu.Unlock()
	value, err := fetch()
	if err != nil {
		return value, err
	}
	e.mu.Lock()
	prune(m, func(c cached[T]) time.Time { return c.at }, castTTL)
	m[id] = cached[T]{value: value, at: time.Now()}
	e.mu.Unlock()
	return value, nil
}

func (e *Enricher) Person(ctx context.Context, malID int) (metadata.Person, error) {
	return cachedFetch(e, e.people, malID, func() (metadata.Person, error) { return e.meta.PersonPage(ctx, malID) })
}

func (e *Enricher) Character(ctx context.Context, malID int) (metadata.CharacterPage, error) {
	return cachedFetch(e, e.characters, malID, func() (metadata.CharacterPage, error) { return e.meta.CharacterDetail(ctx, malID) })
}

type castEntry struct {
	cast []metadata.Character
	at   time.Time
}

type extraEntry struct {
	extra metadata.Extra
	at    time.Time
}

func (x extraEntry) fresh() bool {
	if x.extra.StaffMissing {
		return time.Since(x.at) < partialTTL
	}
	return time.Since(x.at) < castTTL
}

func (e *Enricher) Extra(ctx context.Context, animeID int) (metadata.Extra, error) {
	e.mu.Lock()
	if hit, ok := e.extra[animeID]; ok && hit.fresh() {
		e.mu.Unlock()
		return hit.extra, nil
	}
	e.mu.Unlock()

	malID, err := e.store.MalID(ctx, animeID)
	if err != nil || malID == 0 {
		return metadata.Extra{}, err
	}

	extra, err := e.meta.Extra(ctx, malID)
	if err != nil {
		return metadata.Extra{}, err
	}

	if extra.StaffMissing {
		e.log.Warn("show staff unavailable", "anime", animeID)
	}
	e.mu.Lock()
	prune(e.extra, func(x extraEntry) time.Time { return x.at }, castTTL)
	e.extra[animeID] = extraEntry{extra: extra, at: time.Now()}
	e.mu.Unlock()
	return extra, nil
}

func (e *Enricher) Characters(ctx context.Context, animeID int) ([]metadata.Character, error) {
	e.mu.Lock()
	if hit, ok := e.cast[animeID]; ok && time.Since(hit.at) < castTTL {
		e.mu.Unlock()
		return hit.cast, nil
	}
	e.mu.Unlock()

	malID, err := e.store.MalID(ctx, animeID)
	if err != nil || malID == 0 {
		return nil, err
	}

	cast, err := e.meta.Characters(ctx, malID)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	prune(e.cast, func(c castEntry) time.Time { return c.at }, castTTL)
	e.cast[animeID] = castEntry{cast: cast, at: time.Now()}
	e.mu.Unlock()
	return cast, nil
}

func (e *Enricher) Episodes(ctx context.Context, animeID int) (int, error) {
	var saved int

	if e.store.EpisodesStale(ctx, animeID, episodeRefresh) {
		mapping, err := e.meta.Episodes(ctx, animeID)
		if err != nil {
			return 0, err
		}
		if len(mapping.Episodes) > 0 {
			if err := e.store.EnsureAnime(ctx, animeID); err != nil {
				return 0, err
			}
			if saved, err = e.store.SaveEpisodes(ctx, animeID, mapping.Episodes); err != nil {
				return 0, err
			}
		}
		e.stillsAsync(animeID)

		if err := e.store.MarkEpisodesFetched(ctx, animeID, saved); err != nil {
			e.log.Warn("mark episodes fetched", "anime", animeID, "err", err)
		}
	}

	e.flagsAsync(animeID)
	return saved, nil
}

var episodeLabel = regexp.MustCompile(`(?i)(?:episode|ep\.?|第)\s*0*(\d+)`)

func (e *Enricher) stillsAsync(animeID int) {
	if e.ani == nil {
		return
	}
	e.mu.Lock()
	if _, busy := e.stilling[animeID]; busy {
		e.mu.Unlock()
		return
	}
	e.stilling[animeID] = struct{}{}
	e.mu.Unlock()

	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.stilling, animeID)
			e.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		e.fillStills(ctx, animeID)
	}()
}

func (e *Enricher) fillStills(ctx context.Context, animeID int) {
	if e.ani == nil {
		return
	}

	missing, err := e.store.StillGaps(ctx, animeID)
	if err != nil || len(missing) == 0 {
		return
	}

	listed, err := e.ani.StreamingEpisodes(ctx, animeID)
	if err != nil || len(listed) == 0 {
		return
	}

	found := map[int]string{}
	for _, ep := range listed {
		match := episodeLabel.FindStringSubmatch(ep.Title)
		if match == nil || ep.Thumbnail == "" {
			continue
		}
		if number, _ := strconv.Atoi(match[1]); missing[number] {
			found[number] = ep.Thumbnail
		}
	}

	n, err := e.store.SetEpisodeStills(ctx, animeID, found)
	if err != nil {
		e.log.Warn("episode stills", "anime", animeID, "err", err)
		return
	}
	if n > 0 {
		e.log.Info("episode stills filled from streaming listing", "anime", animeID, "count", n)
	}
}

func (e *Enricher) flagsAsync(animeID int) {
	malID, err := e.store.MalID(context.Background(), animeID)
	if err != nil || malID == 0 {
		return
	}

	e.mu.Lock()
	if _, busy := e.fetching[malID]; busy {
		e.mu.Unlock()
		return
	}
	if e.fetching == nil {
		e.fetching = map[int]struct{}{}
	}
	e.fetching[malID] = struct{}{}
	e.mu.Unlock()

	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.fetching, malID)
			e.mu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		crawl := sync.OnceValues(func() (metadata.Listing, error) { return e.meta.Listing(ctx, malID) })
		e.flags(ctx, animeID, malID, crawl)
		e.listingFrom(ctx, animeID, crawl)
		e.englishTitles(ctx, animeID, crawl)
		e.scores(ctx, animeID, malID, crawl)

		e.fillerGaps(ctx, animeID, malID)
	}()
}

func (e *Enricher) flags(ctx context.Context, animeID, malID int, crawl func() (metadata.Listing, error)) {
	if e.store.FlagsFetched(ctx, malID) {
		return
	}

	listed, err := crawl()
	found := listed.Flags
	if errors.Is(err, metadata.ErrNotListed) {
		if err := e.store.MarkFlagsFetched(ctx, malID, false); err != nil {
			e.log.Warn("mark flags fetched", "err", err)
		}
		return
	}
	if err != nil {
		e.log.Warn("episode flags", "anime", animeID, "err", err)
		return
	}

	recaps, err := e.store.SaveFlags(ctx, malID, found)
	if err != nil {
		e.log.Warn("save episode flags", "anime", animeID, "err", err)
		return
	}
	if err := e.store.MarkFlagsFetched(ctx, malID, e.store.Finished(ctx, animeID)); err != nil {
		e.log.Warn("mark flags fetched", "err", err)
	}
	if recaps > 0 {
		e.log.Info("recap episodes found", "anime", animeID, "count", recaps)
	}
}

func (e *Enricher) scores(ctx context.Context, animeID, malID int, crawl func() (metadata.Listing, error)) {
	if !e.store.ScoresDue(ctx, animeID, malID) {
		return
	}
	found, err := crawl()
	if err != nil && !errors.Is(err, metadata.ErrNotListed) {
		e.log.Warn("episode scores", "anime", animeID, "err", err)
		return
	}
	if err := e.store.SaveScores(ctx, malID, found.Scores); err != nil {
		e.log.Warn("save episode scores", "anime", animeID, "err", err)
	}
}

func (e *Enricher) listingFrom(ctx context.Context, animeID int, crawl func() (metadata.Listing, error)) {
	if !e.store.Finished(ctx, animeID) || !e.store.EpisodesBare(ctx, animeID) || e.store.ListingTried(ctx, animeID) {
		return
	}

	found, err := crawl()
	if err != nil && !errors.Is(err, metadata.ErrNotListed) {
		e.log.Warn("episode listing", "anime", animeID, "err", err)
		return
	}
	if total, _ := e.store.EpisodeCount(ctx, animeID); len(found.Episodes) < total {
		found.Episodes = nil
	}
	saved, err := e.store.FillEpisodes(ctx, animeID, found.Episodes)
	if err != nil {
		e.log.Warn("save episode listing", "anime", animeID, "err", err)
		return
	}
	if err := e.store.MarkListingTried(ctx, animeID, saved); err != nil {
		e.log.Warn("mark episode listing", "err", err)
	}
	if saved > 0 {
		e.log.Info("episodes named from the MyAnimeList listing", "anime", animeID, "count", saved)
	}
}

func (e *Enricher) TitlesPending(ctx context.Context, animeID int) bool {
	malID, err := e.store.MalID(ctx, animeID)
	if err != nil || malID == 0 {
		return false
	}
	e.mu.Lock()
	_, busy := e.fetching[malID]
	e.mu.Unlock()
	return busy && e.store.TitlesDue(ctx, animeID)
}

func (e *Enricher) englishTitles(ctx context.Context, animeID int, crawl func() (metadata.Listing, error)) {
	if !e.store.TitlesDue(ctx, animeID) {
		return
	}
	found, err := crawl()
	if err != nil && !errors.Is(err, metadata.ErrNotListed) {
		e.log.Warn("episode titles", "anime", animeID, "err", err)
		if err := e.store.MarkTitlesTried(ctx, animeID, 0); err != nil {
			e.log.Warn("mark episode titles", "err", err)
		}
		return
	}
	filled, err := e.store.FillEnglishTitles(ctx, animeID, found.Episodes)
	if err != nil {
		e.log.Warn("save episode titles", "anime", animeID, "err", err)
		return
	}
	if err := e.store.MarkTitlesTried(ctx, animeID, filled); err != nil {
		e.log.Warn("mark episode titles", "err", err)
	}
	if filled > 0 {
		e.log.Info("english episode titles filled from MyAnimeList", "anime", animeID, "count", filled)
	}
}

func (e *Enricher) fillerGaps(ctx context.Context, animeID, malID int) {
	missing, err := e.store.UnclassifiedEpisodes(ctx, animeID)
	if err != nil || missing == 0 {
		return
	}

	titles, err := e.store.SearchTitles(ctx, animeID)
	if err != nil {
		return
	}
	if english, err := e.store.EnglishTitle(ctx, animeID); err == nil && english != "" {
		titles = append(titles, english)
	}

	slug := e.fillerSlug(ctx, titles)
	if slug == "" {
		return
	}

	rows, err := e.meta.FillerSiteEpisodes(ctx, slug)
	if err != nil {
		e.log.Debug("filler site", "anime", animeID, "slug", slug, "err", err)
		return
	}

	filled, err := e.store.SaveFillerGaps(ctx, malID, rows)
	if err != nil {
		e.log.Warn("save filler gaps", "anime", animeID, "err", err)
		return
	}
	if filled > 0 {
		e.log.Info("filled filler gaps from the site",
			"anime", animeID, "slug", slug, "episodes", filled)
	}
}

func (e *Enricher) fillerSlug(ctx context.Context, titles []string) string {
	e.mu.Lock()
	index, fresh := e.fillerShows, time.Since(e.fillerShowsAt) < fillerRefresh
	e.mu.Unlock()

	if !fresh || index == nil {
		found, err := e.meta.FillerSiteShows(ctx)
		if err != nil {
			e.log.Debug("filler site index", "err", err)
			return ""
		}
		e.mu.Lock()
		e.fillerShows, e.fillerShowsAt = found, time.Now()
		index = found
		e.mu.Unlock()
	}

	for _, title := range titles {
		if slug, ok := index[metadata.NormaliseTitle(title)]; ok {
			return slug
		}
	}
	return ""
}

func (e *Enricher) Skips(ctx context.Context, animeID, episode int, durationSeconds int) error {
	malID, err := e.store.MalID(ctx, animeID)
	if err != nil || malID == 0 {
		return err
	}
	if durationSeconds <= 0 {
		return nil
	}
	key := [2]int{animeID, episode}
	e.mu.Lock()
	if time.Since(e.skipsAsked[key]) < skipsAskEvery {
		e.mu.Unlock()
		return nil
	}
	e.skipsAsked[key] = time.Now()
	e.mu.Unlock()

	skips, err := e.meta.Skips(ctx, malID, episode, durationSeconds)
	if err != nil || len(skips) == 0 {
		return err
	}
	return e.store.SaveSkips(ctx, malID, episode, skips)
}

const (
	fillerRefresh = 7 * 24 * time.Hour
	seadexRefresh = 3 * 24 * time.Hour
	skipsAskEvery = 10 * time.Minute

	episodeRefresh = 6 * time.Hour
)

func (e *Enricher) SeaDex(ctx context.Context, force bool) (int, error) {
	if !force {
		at, err := e.store.SourceRefreshedAt(ctx, "seadex")
		if err == nil && !at.IsZero() && time.Since(at) < seadexRefresh {
			return 0, nil
		}
	}

	releases, err := e.meta.SeaDex(ctx)
	if err != nil {
		return 0, err
	}

	n, err := e.store.SaveSeaDex(ctx, releases)
	if err != nil {
		return 0, err
	}
	e.log.Info("seadex mirrored", "releases", n)
	return n, e.store.MarkSource(ctx, "seadex", n)
}

const tiersRefresh = 7 * 24 * time.Hour

func (e *Enricher) GroupTiers(ctx context.Context, force bool) (int, error) {
	if !force {
		at, err := e.store.SourceRefreshedAt(ctx, "trash")
		if err == nil && !at.IsZero() && time.Since(at) < tiersRefresh {
			return 0, e.UseGroupTiers(ctx)
		}
	}
	tiers, err := e.meta.GroupTiers(ctx)
	if err != nil {
		return 0, errors.Join(err, e.UseGroupTiers(ctx))
	}
	if err := e.store.SaveGroupTiers(ctx, tiers); err != nil {
		return 0, err
	}
	e.log.Info("release group tiers mirrored", "groups", len(tiers))
	if err := e.store.MarkSource(ctx, "trash", len(tiers)); err != nil {
		return 0, err
	}
	return len(tiers), e.UseGroupTiers(ctx)
}

func (e *Enricher) UseGroupTiers(ctx context.Context) error {
	saved, err := e.store.GroupTiers(ctx)
	if err != nil {
		return err
	}
	tiers := make([]score.GroupTier, 0, len(saved))
	for _, t := range saved {
		tiers = append(tiers, score.NewGroupTier(t.Kind, t.Level, t.Name, t.Pattern))
	}
	score.SetGroupTiers(tiers)
	return nil
}

func (e *Enricher) Fillers(ctx context.Context, force bool) (int, error) {
	if !force {
		at, err := e.store.SourceRefreshedAt(ctx, "filler")
		if err == nil && !at.IsZero() && time.Since(at) < fillerRefresh {
			return 0, nil
		}
	}

	rows, err := e.meta.Fillers(ctx)
	if err != nil {
		return 0, err
	}

	n, err := e.store.SaveFillers(ctx, rows)
	if err != nil {
		return 0, err
	}
	e.log.Info("filler data refreshed", "episodes", n)
	return n, e.store.MarkSource(ctx, "filler", n)
}
