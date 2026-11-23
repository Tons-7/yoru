package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yoru/internal/db"
	"yoru/internal/metadata"
	"yoru/internal/store"
)

func main() {
	path := flag.String("db", "", "database to seed")
	anime := flag.Int("anime", 0, "anime id the episodes belong to")
	pack := flag.Bool("pack", false, "also seed a two-episode season pack")
	extras := flag.Bool("extras", false, "also mark episode 2 filler and add a release notification")
	flag.Parse()
	if *path == "" || *anime <= 0 {
		fmt.Fprintln(os.Stderr, "usage: e2e-seed -db yoru.db -anime 127230 [-pack] [-extras]")
		os.Exit(2)
	}
	if abs, err := filepath.Abs(*path); err != nil || !strings.HasPrefix(strings.ToLower(abs), strings.ToLower(filepath.Clean(os.TempDir()))+string(filepath.Separator)) {
		fmt.Fprintln(os.Stderr, "e2e-seed only writes to a scratch database under the temp folder")
		os.Exit(2)
	}
	if err := seed(*path, *anime, *pack, *extras); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func seed(path string, anime int, withPack, withExtras bool) error {
	conn, err := db.Open(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Migrate(); err != nil {
		return err
	}
	st := store.New(conn)
	ctx := context.Background()

	rows := []struct {
		tag     string
		episode int
		kept    bool
	}{{"cached", 2, false}, {"kept", 3, true}}
	for _, r := range rows {
		hash := strings.ToLower(fmt.Sprintf("e2e%037x", r.episode))
		name := fmt.Sprintf("[E2E] Yoru Test Show - %02d [1080p].mkv", r.episode)
		const size = 700 << 20
		if err := st.RecordTorrent(ctx, store.TorrentRecord{
			InfoHash: hash, Name: name, TotalSize: size, AnimeID: anime,
			EpKey: fmt.Sprint(r.episode), FilePath: name,
		}); err != nil {
			return err
		}
		if err := st.SetCacheBytes(ctx, hash, 0, size, true); err != nil {
			return err
		}
		if r.kept {
			if _, err := st.KeepDownload(ctx, hash, true); err != nil {
				return err
			}
		}
	}

	if withExtras {
		malID, err := st.MalID(ctx, anime)
		if err != nil || malID == 0 {
			return fmt.Errorf("anime %d has no mal id yet; open its page first", anime)
		}
		if _, err := st.SaveFillers(ctx, []metadata.Filler{{MalID: malID, Episode: 2, Kind: "filler"}}); err != nil {
			return err
		}
		if _, err := st.AddNotification(ctx, store.Notification{
			Kind: store.NotifyRelease, AnimeID: &anime, Episode: new(int(7)),
			Title: "Episode 7 is available", Body: "[E2E] Yoru Test Show - 07 [1080p].mkv",
		}); err != nil {
			return err
		}
	}
	if !withPack {
		return nil
	}
	pack := strings.ToLower(fmt.Sprintf("e2e%037x", 999))
	for i, episode := range []int{4, 5} {
		if err := st.RecordTorrent(ctx, store.TorrentRecord{
			InfoHash: pack, Name: "[E2E] Yoru Test Show - Batch [1080p]", TotalSize: 700 << 20,
			AnimeID: anime, EpKey: fmt.Sprint(episode), FileIndex: i, FilePath: fmt.Sprintf("%02d.mkv", episode),
		}); err != nil {
			return err
		}
		if err := st.SetCacheBytes(ctx, pack, i, 700<<20, true); err != nil {
			return err
		}
	}
	return nil
}
