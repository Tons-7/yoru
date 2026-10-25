package transcode

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"yoru/internal/sound"
)

// Caps: what a device says it decodes smoothly; nil (an older player, a TV) keeps the any-browser plan.
type Caps struct {
	// Codecs as ffprobe names them, "-10" for over 8 bits: "hevc", "hevc-10", "h264-10", "av1-10", "eac3".
	codecs map[string]bool
	// Channels is how many speakers the device plays to; 0 when it can't say.
	Channels int
	// HDR: its screen shows HDR, so an HDR picture it decodes goes as it is, not tone-mapped.
	HDR bool
}

// Want is what the viewer asks of the picture: a cap for data or a slow link (0 none), flashing softened.
type Want struct {
	Height, Rate int
	Soften       bool
}

// Quality steps a device can cap its stream at: height, and bits a second for the picture.
var Steps = map[string][2]int{"1080": {1080, 6_000_000}, "720": {720, 3_000_000}, "480": {480, 1_200_000}}

// ParseCaps reads the player's list ("hevc,hevc-10,av1,flac,ch6"); "" is nil, unknown words are ignored.
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

// Decodes: plays this codec at this depth (0 for audio); every device plays H.264 8-bit, AAC and MP3.
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

// variantID: the episode's id for the any-browser plan, else one per plan, so devices never wipe each other's.
func variantID(id string, info *MediaInfo, encoder string, caps *Caps, want Want) string {
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

// planKey sums up what caps and wants decide for this file: the picture, and each audio track's way out.
func planKey(info *MediaInfo, encoder string, caps *Caps, want Want) string {
	var b strings.Builder
	for track := range max(len(info.Audio), 1) {
		p := planWith(info, encoder, track, sound.Options{}, caps, want)
		p.Sound, p.Reason, p.VideoCodec, p.ForDevice = sound.Options{}, "", "", false
		fmt.Fprintf(&b, "%+v;", p)
	}
	return b.String()
}

// BaseID is the episode's session id under a variant's ("<anime>-<episode>-dev…", "-burn…").
func BaseID(id string) string {
	for i := 0; i+1 < len(id); i++ {
		if id[i] == '-' && (id[i+1] >= 'a' && id[i+1] <= 'z') {
			return id[:i]
		}
	}
	return id
}
