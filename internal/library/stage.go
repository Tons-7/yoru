package library

import (
	"fmt"
	"strings"
)

type Stage struct {
	Step    string `json:"step"`
	Release string `json:"release,omitempty"`
	Peers   int    `json:"peers,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`

	rank int
}

func stageKey(animeID, episode int) string { return fmt.Sprintf("%d/%d", animeID, episode) }

func (p *Playback) SetStage(animeID, episode int, s Stage) { p.setStage(animeID, episode, 0, s) }

func (p *Playback) EnterStage(animeID, episode int, s Stage) {
	p.stageMu.Lock()
	if p.stageHeld == nil {
		p.stageHeld = map[string]int{}
	}
	p.stageHeld[stageKey(animeID, episode)]++
	p.stageMu.Unlock()
	p.setStage(animeID, episode, 0, s)
}

func (p *Playback) setStage(animeID, episode, rank int, s Stage) {
	p.stageMu.Lock()
	defer p.stageMu.Unlock()
	if p.stages == nil {
		p.stages = map[string]Stage{}
	}
	key := stageKey(animeID, episode)
	have, ok := p.stages[key]
	if !ok && rank > 0 {
		return
	}
	if ok && have.Step != "search" && have.rank < rank {
		return
	}
	s.rank = rank
	p.stages[key] = s
}

func (p *Playback) dropStage(animeID, episode, rank int) {
	p.stageMu.Lock()
	defer p.stageMu.Unlock()
	key := stageKey(animeID, episode)
	if have, ok := p.stages[key]; ok && have.rank == rank {
		p.stages[key] = Stage{Step: "search"}
	}
}

func (p *Playback) ClearStage(animeID, episode int) {
	p.stageMu.Lock()
	defer p.stageMu.Unlock()
	key := stageKey(animeID, episode)
	if p.stageHeld[key] > 1 {
		p.stageHeld[key]--
		return
	}
	delete(p.stageHeld, key)
	delete(p.stages, key)
}

func (p *Playback) startingShow(animeID int) bool {
	if p == nil {
		return false
	}
	prefix := fmt.Sprintf("%d/", animeID)
	p.stageMu.Lock()
	defer p.stageMu.Unlock()
	for k := range p.stages {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

func (p *Playback) Stage(animeID, episode int) (Stage, bool) {
	if p == nil {
		return Stage{}, false
	}
	p.stageMu.Lock()
	defer p.stageMu.Unlock()
	s, ok := p.stages[stageKey(animeID, episode)]
	return s, ok
}
