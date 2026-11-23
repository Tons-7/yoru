package transcode

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"yoru/internal/sound"
)

type Caps struct {
	codecs   map[string]bool
	Channels int
	HDR      bool
}

type Want struct {
	Height, Rate int
	Soften       bool
	Keep         bool
}

var Steps = map[string][2]int{"1080": {1080, 6_000_000}, "720": {720, 3_000_000}, "480": {480, 1_200_000}}

func ParseCaps(list string) *Caps {
	if list == "" {
		return nil
	}
	c := &Caps{codecs: map[string]bool{}}
	for word := range strings.SplitSeq(strings.ToLower(list), ",") {
		word = strings.TrimSpace(word)
		if n, ok := strings.CutPrefix(word, "ch"); ok {
			c.Channels, _ = strconv.Atoi(n)
			continue
		}
		if word == "hdr" {
			c.HDR = true
			continue
		}
		c.codecs[word] = true
	}
	return c
}

func (c *Caps) Decodes(codec string, depth int) bool {
	key := codec
	if depth > 8 {
		key += "-10"
	}
	switch key {
	case "h264", "aac", "mp3":
		return true
	}
	return c != nil && c.codecs[key]
}

func variantID(id string, info *MediaInfo, encoder string, caps *Caps, want Want) string {
	if want.Keep {
		want.Keep = false
		return variantID(id, info, encoder, caps, want) + "-keep"
	}
	if caps == nil && want == (Want{}) {
		return id
	}
	mine, theirs := planKey(info, encoder, caps, want), planKey(info, encoder, nil, Want{})
	if mine == theirs {
		return id
	}
	h := fnv.New32a()
	h.Write([]byte(mine))
	return fmt.Sprintf("%s-dev%08x", id, h.Sum32())
}

func planKey(info *MediaInfo, encoder string, caps *Caps, want Want) string {
	var b strings.Builder
	for track := range max(len(info.Audio), 1) {
		p := planWith(info, encoder, track, sound.Options{}, caps, want)
		p.Sound, p.Reason, p.VideoCodec, p.ForDevice = sound.Options{}, "", "", false
		fmt.Fprintf(&b, "%+v;", p)
	}
	return b.String()
}

func BaseID(id string) string {
	for i := 0; i+1 < len(id); i++ {
		if id[i] == '-' && (id[i+1] >= 'a' && id[i+1] <= 'z') {
			return id[:i]
		}
	}
	return id
}
