package library

import (
	"context"
	"testing"

	"yoru/internal/store"
)

func TestSyncWithPullsOffStillPushes(t *testing.T) {
	up := &aniServer{list: `{"data":{"MediaListCollection":{"lists":[
      {"name":"Watching","entries":[
        {"id":78,"mediaId":200,"progress":1,"status":"PLANNING","updatedAt":2000,
         "media":{"id":200,"title":{"romaji":"Other"},"episodes":12}}]}]}}}`}
	sync, st := newAniSync(t, up)
	ctx := context.Background()
	pulls := false
	sync.WithPulls(func(context.Context) bool { return pulls })

	st.ImportList(ctx, []store.Anime{{ID: 100, Romaji: "Frieren"}}, nil, store.ImportMerge)
	st.MarkWatched(ctx, 100, 3)

	rep, err := sync.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pushed != 1 || rep.Pulled != 0 {
		t.Fatalf("report = %+v, want the push and no pull", rep)
	}
	if e, _ := st.ListEntry(ctx, 200); e.AnimeID == 200 {
		t.Fatal("the site's list was pulled with checking turned off")
	}

	pulls = true
	if rep, err := sync.Run(ctx); err != nil || rep.Pulled != 1 {
		t.Fatalf("report = %+v, err %v, want the pull back", rep, err)
	}
}
