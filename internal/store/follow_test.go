package store

import (
	"context"
	"testing"
)

func TestNotifiedOncePerEpisode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)

	if err := s.RecordNotified(ctx, "hash-subsplease", 1, 7); err != nil {
		t.Fatal(err)
	}

	seen, _ := s.AlreadyNotified(ctx, "hash-erai", 1, 7)
	if !seen {
		t.Error("a second group's release of episode 7 announced it again")
	}

	next, _ := s.AlreadyNotified(ctx, "hash-other", 1, 8)
	if next {
		t.Error("episode 8 was suppressed by episode 7")
	}

	other, _ := s.AlreadyNotified(ctx, "hash-other", 2, 7)
	if other {
		t.Error("another show's episode 7 was suppressed")
	}
}

func TestADoubleEpisodeReleaseCountsForBoth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	if err := s.RecordNotified(ctx, "hash-0102", 1, 1); err != nil {
		t.Fatal(err)
	}
	if seen, _ := s.AlreadyNotified(ctx, "hash-0102", 1, 2); seen {
		t.Fatal("episode 2 was skipped because its release already announced episode 1")
	}
	if err := s.RecordNotified(ctx, "hash-0102", 1, 2); err != nil {
		t.Fatal(err)
	}
	if !s.EpisodeFetched(ctx, 1, 2) {
		t.Error("episode 2 is not recorded under the shared release")
	}
}

func TestUnknownEpisodeStillDedupesByHash(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)

	if err := s.RecordNotified(ctx, "hash-a", 1, 7); err != nil {
		t.Fatal(err)
	}
	if seen, _ := s.AlreadyNotified(ctx, "hash-a", 0, 0); !seen {
		t.Error("the same release was announced twice")
	}
}

func TestFollowIsOnlyForShowsBeingWatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)

	if err := s.SetFollow(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if !hasFollow(mustFollows(t, s), 1) {
		t.Fatal("following did not take")
	}

	if err := s.SetFollow(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if hasFollow(mustFollows(t, s), 1) {
		t.Fatal("unfollowing left the show subscribed")
	}
}

func TestAShowBeingWatchedIsFollowedWithoutAsking(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for id := 1; id <= 3; id++ {
		seedAnime(t, s, id)
	}
	for id, status := range map[int]string{1: StatusCurrent, 2: "PLANNING", 3: "COMPLETED"} {
		if err := s.SetListStatus(ctx, id, status, -1); err != nil {
			t.Fatal(err)
		}
	}
	for id := 1; id <= 3; id++ {
		if err := s.Unfollow(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	follows := mustFollows(t, s)
	if !hasFollow(follows, 1) {
		t.Error("a show being watched is not followed")
	}
	if hasFollow(follows, 2) || hasFollow(follows, 3) {
		t.Errorf("planned or finished shows are followed: %+v", follows)
	}
}

func TestAMutedShowIsOffDespiteAutoDownload(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 2)
	if err := s.SetAnimePref(ctx, 2, "autodownload.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.FollowOf(ctx, 2); !f.On {
		t.Fatal("auto-download alone doesn't alert")
	}
	if err := s.SetNotify(ctx, 2, false); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.FollowOf(ctx, 2); f.On {
		t.Error("a show muted on its page still alerts, by its auto-download")
	}
}

func mustFollows(t *testing.T, s *Store) []Follow {
	t.Helper()
	out, err := s.Follows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out
}
