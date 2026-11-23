package transcode

import (
	"regexp"
)

var (
	englishTag  = regexp.MustCompile(`(?i)^(en|eng|english)$|\benglish\b|\bdub\b`)
	japaneseTag = regexp.MustCompile(`(?i)^(ja|jp|jpn|japanese)$|\bjapanese\b`)
)

func ChooseAudio(info *MediaInfo, want string) int {
	if info == nil || len(info.Audio) < 2 {
		return 0
	}
	var lang *regexp.Regexp
	switch want {
	case "dub":
		lang = englishTag
	case "sub":
		lang = japaneseTag
	}
	if lang != nil {
		for i, a := range info.Audio {
			if lang.MatchString(a.Language) {
				return i
			}
		}
		for i, a := range info.Audio {
			if lang.MatchString(a.Title) {
				return i
			}
		}
	}
	for i, a := range info.Audio {
		if a.Default {
			return i
		}
	}
	return 0
}

func (s *Session) AudioTrack() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.audioTrack
}

func (s *Session) ApplyAudioPreference(want string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if want == "" {
		want = "either"
	}
	if s.audioPref == want {
		return false
	}
	s.audioPref = want
	return true
}

func (s *Session) SetAudioTrack(n int) bool {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	if n < 0 || n >= len(s.Info.Audio) || n == s.audioTrack || s.closed {
		s.mu.Unlock()
		return false
	}
	s.audioTrack = n
	s.replan()
	s.headFrom, s.headTo = -1, -1
	s.barren, s.broken = 0, nil
	s.mu.Unlock()

	s.stop()
	s.wipeOutput()
	s.log.Info("audio track switched", "session", s.ID, "track", n)
	return true
}
