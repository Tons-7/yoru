package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemovingTheMiddleEpisodeKeepsItsNeighboursWhole(t *testing.T) {
	f := newFixture(t)
	c := ctx(t)
	in, err := f.client.Inspect(c, f.magnet)
	if err != nil {
		t.Fatal(err)
	}
	var id int
	for _, file := range in.Details.Files {
		added, err := f.client.Add(c, f.magnet, file)
		if err != nil {
			t.Fatal(err)
		}
		id = added.ID
	}
	if err := f.client.Await(c, id, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	if err := f.client.DeleteFile(c, id, 1); err != nil {
		t.Fatal(err)
	}
	pack := filepath.Join(f.cache, "Show Pack")
	two, _ := os.ReadFile(filepath.Join(pack, "Show - 02.mkv"))
	want := f.content["Show - 02.mkv"]
	if len(two) != len(want) {
		t.Fatalf("the removed file is %d bytes, want it kept at %d with its middle freed", len(two), len(want))
	}
	if !bytes.Equal(two[:100], want[:100]) || !bytes.Equal(two[len(two)-100:], want[len(want)-100:]) {
		t.Error("the bytes the neighbours share with the removed episode were lost")
	}
	mid := len(two) / 2
	if !bytes.Equal(two[mid:mid+1000], make([]byte, 1000)) {
		t.Error("the middle of the removed episode is still on disk")
	}

	check := func(when string) {
		s, err := f.client.Stats(c, id)
		if err != nil || !s.Finished {
			t.Fatalf("%s: the pack is no longer finished (%v)", when, err)
		}
		for _, name := range []string{"Show - 01.mkv", "Show - 03.mkv"} {
			if got, _ := os.ReadFile(filepath.Join(pack, name)); !bytes.Equal(got, f.content[name]) {
				t.Errorf("%s: %s changed", when, name)
			}
		}
	}
	check("right after")
	f.engine.Stop()
	f.start(t)
	time.Sleep(time.Second)
	live, err := f.client.Live(c)
	if err != nil || len(live) != 1 {
		t.Fatalf("after a restart: %v %v", live, err)
	}
	for _, again := range live {
		id = again
	}
	if err := f.client.Await(c, id, 10*time.Second); err != nil {
		t.Fatalf("after a restart the neighbours need fetching again: %v", err)
	}
	check("after a restart")
}

func TestOneEpisodeOfAPackPausesAndGoesAlone(t *testing.T) {
	f := newFixture(t)
	c := ctx(t)
	in, err := f.client.Inspect(c, f.magnet)
	if err != nil {
		t.Fatal(err)
	}
	added, err := f.client.Add(c, f.magnet, in.Details.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Add(c, f.magnet, in.Details.Files[1]); err != nil {
		t.Fatal(err)
	}

	if err := f.client.IncludeFile(c, added.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.client.Details(c, added.ID); !d.Files[0].Included || d.Files[1].Included {
		t.Fatalf("after pausing episode 2: %+v", d.Files)
	}
	if err := f.client.Await(c, added.ID, 10*time.Second); err != nil {
		t.Fatalf("episode 1 stopped with episode 2: %v", err)
	}
	one := filepath.Join(f.cache, "Show Pack", "Show - 01.mkv")
	if got, _ := os.ReadFile(one); !bytes.Equal(got, f.content["Show - 01.mkv"]) {
		t.Fatal("episode 1 did not finish whole")
	}

	if err := f.client.IncludeFile(c, added.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Await(c, added.ID, 10*time.Second); err != nil {
		t.Fatalf("episode 2 did not finish once resumed: %v", err)
	}

	if err := f.client.DeleteFile(c, added.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(one); len(got) == 0 || !bytes.Equal(got[:1000], make([]byte, 1000)) {
		t.Error("removing episode 1 left its data; only the piece it shares with episode 2 should stay")
	}
	two := filepath.Join(f.cache, "Show Pack", "Show - 02.mkv")
	if got, _ := os.ReadFile(two); !bytes.Equal(got, f.content["Show - 02.mkv"]) {
		t.Error("removing episode 1 touched episode 2")
	}
	if live, _ := f.client.Live(c); len(live) != 1 {
		t.Errorf("the pack went with one episode: %v", live)
	}
	if d, _ := f.client.Details(c, added.ID); d.Files[0].Included || !d.Files[1].Included {
		t.Errorf("after removing episode 1: %+v", d.Files)
	}
}
