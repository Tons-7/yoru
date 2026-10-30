package server

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/jobs"
	"yoru/internal/library"
	"yoru/internal/store"
)

func (s *Server) jobStatus(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduler unavailable"})
		return
	}
	send(w, http.StatusOK, map[string]any{"jobs": s.jobs.Status()})
}

func (s *Server) runJob(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "scheduler unavailable"})
		return
	}
	err := s.jobs.Trigger(r.PathValue("name"))
	switch {
	case errors.Is(err, jobs.ErrRunning):
		send(w, http.StatusConflict, map[string]any{"error": "already running"})
	case err != nil:
		send(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	default:
		send(w, http.StatusAccepted, map[string]any{"started": r.PathValue("name")})
	}
}

type scheduleItem struct {
	AnimeID    int     `json:"animeId"`
	Episode    int     `json:"episode"`
	AiringAt   int     `json:"airingAt"`
	Title      string  `json:"title"`
	Romaji     string  `json:"romaji"`
	English    *string `json:"english,omitempty"`
	Cover      *string `json:"cover,omitempty"`
	CoverLarge *string `json:"coverLarge,omitempty"`
	Thumb      *string `json:"thumb,omitempty"`
	Colour     *string `json:"colour,omitempty"`
	Format     *string `json:"format,omitempty"`
	OnList     bool    `json:"onList"`
	Alerts     bool    `json:"alerts"`
	Progress   int     `json:"progress"`
	Behind     int     `json:"behind"`
	Watched    bool    `json:"watched"`
	Out        bool    `json:"out"`
	Watch      bool    `json:"watch,omitempty"`
	CheckedAt  int64   `json:"checkedAt,omitempty"`
	Position   float64 `json:"position,omitempty"`
	Duration   float64 `json:"duration,omitempty"`

	DelayedFrom int64 `json:"delayedFrom,omitempty"`

	JSTWeekday string `json:"jstWeekday"`

	Show *discoverItem `json:"show,omitempty"`
}

type scheduleDay struct {
	Date    string         `json:"date"`
	Weekday string         `json:"weekday"`
	Today   bool           `json:"today"`
	Items   []scheduleItem `json:"items"`
}

var jst = time.FixedZone("JST", 9*3600)

func zoneFor(offsetMinutes int) *time.Location {
	return time.FixedZone("local", offsetMinutes*60)
}

func midnight(t time.Time, offsetMinutes int) time.Time {
	local := t.In(zoneFor(offsetMinutes))
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

func groupByDay(items []scheduleItem, offsetMinutes int, start int64, count int) []scheduleDay {
	if count < 0 {
		count = 0
	} else if count > 28 {
		count = 28
	}
	zone := zoneFor(offsetMinutes)
	today := time.Now().In(zone).Format("2006-01-02")

	days := make([]scheduleDay, 0, count)
	index := map[string]int{}

	cursor := time.Unix(start, 0).In(zone)
	for range count {
		date := cursor.Format("2006-01-02")
		index[date] = len(days)
		days = append(days, scheduleDay{
			Date:    date,
			Weekday: cursor.Format("Monday"),
			Today:   date == today,
			Items:   []scheduleItem{},
		})
		cursor = cursor.AddDate(0, 0, 1)
	}

	for _, item := range items {
		date := time.Unix(int64(item.AiringAt), 0).In(zone).Format("2006-01-02")
		if i, ok := index[date]; ok {
			days[i].Items = append(days[i].Items, item)
		}
	}
	return days
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 || days > 28 {
		days = 7
	}

	offset, grouped := 0, false
	if raw := q.Get("tz"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= -1440 && v <= 1440 {
			offset, grouped = v, true
		}
	}

	start := time.Now().Add(-24 * time.Hour).Unix()
	if v, err := strconv.ParseInt(q.Get("start"), 10, 64); err == nil && v > 0 {
		start = v
	} else if grouped {
		start = time.Now().Unix()
	}
	if grouped {
		start = midnight(time.Unix(start, 0), offset).Unix()
	}
	end := start + int64(days)*24*3600

	entries, err := s.anilist.Schedule(r.Context(), start, end)
	if err != nil {
		s.fail(w, "schedule", err)
		return
	}
	media := make([]anilist.Media, len(entries))
	for n, e := range entries {
		media[n] = e.Media
	}
	shows := s.decorate(r, media)

	onList, err := s.store.ListProgress(r.Context())
	if err != nil {
		s.log.Warn("list progress", "err", err)
	}
	global, err := s.store.Prefs(r.Context(), 0)
	alertsOn := grouped && err == nil && global.String("notify.enabled") != "false"
	adultOK := err == nil && global.Bool("home.adult_picks")
	alerting := map[int]bool{}
	alerts := func(id int) bool {
		if !alertsOn {
			return false
		}
		on, seen := alerting[id]
		if !seen {
			f, _ := s.store.FollowOf(r.Context(), id)
			on = f.On
			alerting[id] = on
		}
		return on
	}

	mineOnly := q.Get("mine") == "true"
	titles := s.store.TitleMode(r.Context(), 0)
	items := make([]scheduleItem, 0, len(entries))
	coverage := map[int]library.Coverage{}
	if s.watcher != nil {
		coverage = s.watcher.Coverage(r.Context())
	}
	known, listedIDs := map[int]bool{}, map[int]bool{}
	for _, e := range entries {
		_, listed := onList[e.MediaID]
		_, watching := coverage[e.MediaID]
		if listed || watching {
			known[e.MediaID] = true
		}
		if listed {
			listedIDs[e.MediaID] = true
		}
	}
	ready, err := s.store.ReadyEpisodes(r.Context(), slices.Collect(maps.Keys(known)))
	if err != nil {
		s.log.Warn("ready episodes", "err", err)
	}
	resume, err := s.store.ResumePoints(r.Context(), slices.Collect(maps.Keys(listedIDs)))
	if err != nil {
		s.log.Warn("resume points", "err", err)
	}
	now := time.Now().Unix()

	for n, e := range entries {
		progress, listed := onList[e.MediaID]
		if mineOnly && !listed || e.Media.IsAdult && !listed && !adultOK {
			continue
		}

		item := scheduleItem{
			AnimeID:    e.MediaID,
			Episode:    e.Episode,
			AiringAt:   e.AiringAt,
			English:    e.Media.Title.English,
			Cover:      e.Media.CoverImage.Large,
			CoverLarge: e.Media.CoverImage.ExtraLarge,
			Thumb:      e.Media.CoverImage.Medium,
			Colour:     e.Media.CoverImage.Color,
			Format:     e.Media.Format,
			OnList:     listed,
			Alerts:     alerts(e.MediaID),
			Progress:   progress,
			Show:       &shows[n],
		}
		if e.Media.Title.Romaji != nil {
			item.Romaji = *e.Media.Title.Romaji
		}
		item.Title = store.PickTitle(titles, item.Romaji, item.English)
		item.JSTWeekday = time.Unix(int64(e.AiringAt), 0).In(jst).Format("Monday")

		if listed && e.Episode > 1 {
			if behind := (e.Episode - 1) - progress; behind > 0 {
				item.Behind = behind
			}
		}
		item.Watched = listed && progress >= e.Episode
		key := [2]int{e.MediaID, e.Episode}
		if !item.Watched {
			aired := int64(e.AiringAt) <= now
			item.Out = aired && ready[key]
			if c, ok := coverage[e.MediaID]; ok && !item.Out && aired && c.Searches(e.Episode) {
				item.Watch = true
				if at, ok := s.watcher.CheckedAt(e.MediaID, e.Episode); ok {
					item.CheckedAt = at.Unix()
				}
			}
			if p, ok := resume[key]; ok {
				item.Position, item.Duration = p[0], p[1]
			}
		}
		if d, ok := s.store.ShowDelay(r.Context(), e.MediaID); ok && d.Episode == e.Episode {
			item.DelayedFrom = d.WasAt
		}

		items = append(items, item)
	}

	body := map[string]any{
		"items": items,
		"count": len(items),
		"start": start,
		"end":   end,
	}

	if grouped {
		body["days"] = groupByDay(items, offset, start, days)
	}
	send(w, http.StatusOK, body)
}
