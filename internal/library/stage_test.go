package library

import "testing"

func TestStageReportsTheBestRankedCandidate(t *testing.T) {
	p := &Playback{}
	p.SetStage(1, 1, Stage{Step: "search"})
	p.setStage(1, 1, 2, Stage{Step: "files", Release: "third"})
	p.setStage(1, 1, 0, Stage{Step: "data", Release: "first", Peers: 3})
	p.setStage(1, 1, 2, Stage{Step: "data", Release: "third"})

	if s, ok := p.Stage(1, 1); !ok || s.Release != "first" || s.Step != "data" || s.Peers != 3 {
		t.Fatalf("stage = %+v, ok %v", s, ok)
	}

	p.dropStage(1, 1, 0)
	if s, _ := p.Stage(1, 1); s.Step != "search" {
		t.Errorf("after the first failed: %+v", s)
	}
	p.setStage(1, 1, 2, Stage{Step: "data", Release: "third"})
	if s, _ := p.Stage(1, 1); s.Release != "third" {
		t.Errorf("the next candidate was not reported: %+v", s)
	}
}

func TestStageSurvivesALowerCandidateFailing(t *testing.T) {
	p := &Playback{}
	p.setStage(1, 1, 0, Stage{Step: "data", Release: "first"})
	p.dropStage(1, 1, 2)
	if s, _ := p.Stage(1, 1); s.Release != "first" {
		t.Errorf("stage = %+v", s)
	}
}

func TestStageIsPerEpisodeAndCleared(t *testing.T) {
	p := &Playback{}
	p.SetStage(1, 1, Stage{Step: "start"})
	if _, ok := p.Stage(1, 2); ok {
		t.Error("another episode reported a stage")
	}
	p.ClearStage(1, 1)
	if _, ok := p.Stage(1, 1); ok {
		t.Error("the stage outlived the start")
	}
	var none *Playback
	if _, ok := none.Stage(1, 1); ok {
		t.Error("a nil playback reported a stage")
	}
}
