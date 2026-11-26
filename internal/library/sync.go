package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/store"
)

type Sync struct {
	store    *store.Store
	al       *anilist.Client
	mal      *MALSync
	importer *Importer
	pulls    func(context.Context) bool
	log      *slog.Logger

	pushing sync.Mutex
}

func NewSync(s *store.Store, al *anilist.Client, log *slog.Logger) *Sync {
	return &Sync{store: s, al: al, log: log}
}

func (s *Sync) WithMAL(m *MALSync) *Sync {
	s.mal = m
	return s
}

func (s *Sync) WithImporter(i *Importer) *Sync {
	s.importer = i
	return s
}

func (s *Sync) WithPulls(pulls func(context.Context) bool) *Sync {
	s.pulls = pulls
	return s
}

type SyncReport struct {
	Pushed     int `json:"pushed"`
	Failed     int `json:"failed"`
	Pulled     int `json:"pulled"`
	Favourites int `json:"favourites"`
}

const AuthErrorSetting = "anilist.auth_error"

var ErrReconnect = errors.New("anilist rejected the token; reconnect AniList in Settings")

const pushBatch = 100

func (s *Sync) Run(ctx context.Context) (SyncReport, error) {
	rep, err := s.run(ctx)
	if err == nil && s.al.Authenticated() {
		if v, _ := s.store.Setting(ctx, AuthErrorSetting); v != "" {
			s.store.SetSetting(ctx, AuthErrorSetting, "")
		}
	}
	return rep, err
}

func (s *Sync) run(ctx context.Context) (SyncReport, error) {
	var rep SyncReport
	if !s.al.Authenticated() {
		return rep, nil
	}
	ctx = anilist.Background(ctx)

	dirty, err := s.store.DirtyEntries(ctx, pushBatch)
	if err != nil {
		return rep, err
	}

	for _, d := range dirty {
		if err := s.pushLatest(ctx, d.AnimeID, false); err != nil {
			rep.Failed++
			s.log.Warn("push progress", "anime", d.AnimeID, "err", err)

			if errs, ok := err.(anilist.Errors); ok && errs.Unauthorized() {
				s.store.SetSetting(ctx, AuthErrorSetting, "true")
				return rep, ErrReconnect
			}
			if err := s.store.PushFailed(ctx, "anilist", d.AnimeID); err != nil {
				return rep, err
			}
			if anilist.Unreachable(ctx, err) {
				break
			}
			continue
		}
		rep.Pushed++
	}

	if rep.Pushed > 0 {
		s.log.Info("progress synced", "entries", rep.Pushed)
	}

	userID, _ := s.store.SettingInt(ctx, "anilist.user_id")
	if s.importer != nil && userID > 0 && (s.pulls == nil || s.pulls(ctx)) {
		res, err := s.importer.Run(ctx, userID, store.ImportMerge)
		if errs, ok := errors.AsType[anilist.Errors](err); ok && errs.Unauthorized() {
			s.store.SetSetting(ctx, AuthErrorSetting, "true")
			return rep, ErrReconnect
		}
		if err != nil {
			return rep, err
		}
		rep.Pulled = res.Entries
	}

	if userID > 0 {
		held, err := s.favourites(ctx, userID)
		if err != nil {
			s.log.Warn("sync favourites", "err", err)
		} else {
			rep.Favourites = held
		}
	}
	return rep, nil
}

func (s *Sync) favourites(ctx context.Context, userID int) (int, error) {
	remote, err := s.al.Favourites(ctx, userID)
	if err != nil {
		return 0, err
	}

	dirty, err := s.store.DirtyFavourites(ctx)
	if err != nil {
		return 0, err
	}

	on := make(map[int]bool, len(remote))
	for _, id := range remote {
		on[id] = true
	}

	for _, change := range dirty {
		if change.AnimeID <= 0 {
			if err := s.store.MarkFavouriteSynced(ctx, change.AnimeID, change.Favourite); err != nil {
				return 0, err
			}
			continue
		}
		if on[change.AnimeID] != change.Favourite {
			if err := s.al.ToggleFavourite(ctx, change.AnimeID); err != nil {
				s.log.Warn("push favourite", "anime", change.AnimeID, "err", err)
				continue
			}
			on[change.AnimeID] = change.Favourite
		}
		if err := s.store.MarkFavouriteSynced(ctx, change.AnimeID, change.Favourite); err != nil {
			return 0, err
		}
	}

	ids := make([]int, 0, len(on))
	for id, fav := range on {
		if fav {
			ids = append(ids, id)
		}
	}
	held, err := s.store.ApplyRemoteFavourites(ctx, ids)
	if err != nil {
		return 0, err
	}

	if s.importer != nil {
		missing, err := s.store.Unhydrated(ctx, ids)
		if err != nil {
			return held, err
		}
		if _, err := s.importer.Hydrate(ctx, missing); err != nil {
			s.log.Warn("hydrate favourites", "err", err)
		}
	}
	return held, nil
}

func (s *Sync) PushOne(ctx context.Context, animeID int) error {
	var errs []error
	if s.mal != nil {
		if err := s.mal.PushOne(ctx, animeID); err != nil {
			errs = append(errs, err)
		}
	}
	if s.al.Authenticated() && animeID > 0 {
		if err := s.pushLatest(ctx, animeID, true); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Sync) pushLatest(ctx context.Context, animeID int, force bool) error {
	s.pushing.Lock()
	defer s.pushing.Unlock()
	entry, err := s.store.ListEntry(ctx, animeID)
	if err != nil || entry.AnimeID == 0 || (!entry.Dirty && !force) {
		return err
	}
	return s.push(ctx, entry)
}

func (s *Sync) push(ctx context.Context, d store.DirtyEntry) error {
	var completed *anilist.FuzzyDate
	if strings.EqualFold(d.Status, "COMPLETED") {
		completed = fuzzyDate(d.CompletedAt)
	}

	saved, err := s.al.SetProgress(ctx, d.AnimeID, d.Progress, d.Status, completed, d.Repeat, d.Score,
		d.ScoreCleared && d.Score == 0)
	if err != nil {
		return err
	}
	return s.store.ClearDirtyAt(ctx, d.AnimeID, saved.ID, saved.UpdatedAt, d.Changed)
}

func fuzzyDate(s string) *anilist.FuzzyDate {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return nil
	}
	year, err1 := strconv.Atoi(parts[0])
	month, err2 := strconv.Atoi(parts[1])
	day, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || year == 0 {
		return nil
	}
	date := &anilist.FuzzyDate{Year: &year}
	if month > 0 {
		date.Month = &month
		if day > 0 {
			date.Day = &day
		}
	}
	return date
}

func (s *Sync) Watched(ctx context.Context, animeID, episode int) error {
	changed, err := s.store.MarkWatched(ctx, animeID, episode)
	if err != nil || !changed {
		return err
	}
	s.log.Info("episode watched", "anime", animeID, "episode", episode)

	if s.mal != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
			defer cancel()
			if err := s.mal.PushOne(ctx, animeID); err != nil {
				s.log.Warn("push to MyAnimeList", "anime", animeID, "err", err)
			}
		}()
	}
	if s.al.Authenticated() && animeID > 0 {
		return s.pushLatest(ctx, animeID, true)
	}
	return nil
}

func (s *Sync) Remove(ctx context.Context, animeID int) error {
	s.pushing.Lock()
	defer s.pushing.Unlock()
	entry, err := s.store.ListEntry(ctx, animeID)
	if err != nil {
		return err
	}
	if entry.ID > 0 && s.al.Authenticated() {
		if err := s.al.DeleteEntry(ctx, entry.ID); err != nil {
			return fmt.Errorf("remove from AniList: %w", err)
		}
	}
	if s.mal != nil {
		if err := s.mal.Remove(ctx, animeID); err != nil {
			return fmt.Errorf("remove from MyAnimeList: %w", err)
		}
	}
	return s.store.RemoveListEntry(ctx, animeID)
}
