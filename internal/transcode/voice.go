package transcode

import "yoru/internal/sound"

func (s *Session) Sound() sound.Options {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sound
}

func (s *Session) Voice() string { return s.Sound().Level() }

func (s *Session) SetVoice(level string) bool {
	if !sound.ValidVoice(level) {
		return false
	}
	o := s.Sound()
	o.Voice = level
	return s.SetSound(o)
}

func (s *Session) SetSound(o sound.Options) bool {
	o = sound.Normalized(o)
	if !sound.ValidVoice(o.Voice) || !sound.ValidBoost(o.Boost) {
		return false
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	if o == s.sound || s.closed {
		s.mu.Unlock()
		return false
	}
	s.sound = o
	s.replan()
	s.headFrom, s.headTo = -1, -1
	s.barren, s.broken = 0, nil
	s.mu.Unlock()

	s.stop()
	s.wipeOutput()
	s.log.Info("sound switched", "session", s.ID, "voice", o.Level(), "night", o.Night,
		"headphones", o.Headphones, "mono", o.Mono, "boost", o.Boost)
	return true
}

func (s *Session) soundFilter() string {
	channels := 2
	if s.Info != nil && s.audioTrack < len(s.Info.Audio) && s.Info.Audio[s.audioTrack].Channels > 0 {
		channels = s.Info.Audio[s.audioTrack].Channels
	}
	return sound.Chain(s.sound, channels)
}
