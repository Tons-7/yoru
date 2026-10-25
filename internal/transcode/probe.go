// Package transcode serves video to a browser, rewriting Matroska into HLS, usually without re-encoding.
package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"yoru/internal/proc"
	"yoru/internal/sound"
)

type Stream struct {
	Index    int    `json:"index"`
	Kind     string `json:"kind"`
	Codec    string `json:"codec"`
	Profile  string `json:"profile"`
	BitDepth int    `json:"bitDepth"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Channels int    `json:"channels"`
	Language string `json:"language"`
	Title    string `json:"title"`
	Default  bool   `json:"default"`
	Forced   bool   `json:"forced"`
	// The picture's transfer (smpte2084: HDR10, arib-std-b67: HLG), field order and Dolby Vision profile.
	Transfer   string `json:"transfer,omitempty"`
	FieldOrder string `json:"fieldOrder,omitempty"`
	DVProfile  int    `json:"dvProfile,omitempty"`
}

// HDR: a picture made for HDR screens, grey and washed out on any other unless tone-mapped.
func (s Stream) HDR() bool {
	return s.Transfer == "smpte2084" || s.Transfer == "arib-std-b67" || s.DVProfile > 0
}

// Interlaced: fields woven into each frame (DVD-era sources), combed on any progressive screen.
func (s Stream) Interlaced() bool {
	switch s.FieldOrder {
	case "tt", "bb", "tb", "bt":
		return true
	}
	return false
}

type Attachment struct {
	Index    int    `json:"index"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
}

// Chapter is the release's own marker, which beats AniSkip as it fits this exact file.
type Chapter struct {
	Title string  `json:"title"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// "op", "ed", "recap", "preview", "eyecatch" or empty when the title names no part of the episode.
	Kind string `json:"kind"`
}

// Release groups label the same things a dozen ways: "Intro"/"Opening"/"OP", "Credits"/"Ending"/"Outro",
// "Preview"/"Next Episode"/"予告", often numbered. Anything else is body.
func chapterKind(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	// The first word, minus any number: "op", "OP1", "ED 2", "OP - Kick Back".
	var head string
	if fields := strings.FieldsFunc(t, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_'
	}); len(fields) > 0 {
		head = strings.TrimRight(fields[0], "0123456789")
	}

	switch {
	case head == "op", strings.Contains(t, "opening"), strings.Contains(t, "intro"):
		return "op"
	case head == "ed", strings.Contains(t, "ending"),
		strings.Contains(t, "credits"), strings.Contains(t, "outro"):
		return "ed"
	case containsAny(t, "recap", "previously", "前回"):
		return "recap"
	case containsAny(t, "preview", "next episode", "next ep", "next time", "yokoku", "予告"):
		return "preview"
	case containsAny(t, "eyecatch", "eye catch", "eye-catch", "アイキャッチ"):
		return "eyecatch"
	}
	return ""
}

// plausibleSpan is how long a part of this kind runs, in seconds.
func plausibleSpan(kind string) (lo, hi float64) {
	switch kind {
	case "eyecatch":
		return 1, 30
	case "preview":
		return 5, 120
	case "recap":
		return 10, 300
	default:
		return 15, 240
	}
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

type MediaInfo struct {
	Duration    float64      `json:"duration"`
	Container   string       `json:"container"`
	Bitrate     int64        `json:"bitrate"`
	Video       *Stream      `json:"video"`
	Audio       []Stream     `json:"audio"`
	Subtitles   []Stream     `json:"subtitles"`
	Attachments []Attachment `json:"attachments"`
	Chapters    []Chapter    `json:"chapters"`
}

type ffprobeOutput struct {
	Format struct {
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		Index      int    `json:"index"`
		CodecName  string `json:"codec_name"`
		CodecType  string `json:"codec_type"`
		Profile    string `json:"profile"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		Channels   int    `json:"channels"`
		PixFmt     string `json:"pix_fmt"`
		BitsPerRaw string `json:"bits_per_raw_sample"`
		Transfer   string `json:"color_transfer"`
		FieldOrder string `json:"field_order"`
		SideData   []struct {
			Type      string `json:"side_data_type"`
			DVProfile int    `json:"dv_profile"`
		} `json:"side_data_list"`
		Duration    string `json:"duration"`
		Disposition struct {
			Default  int `json:"default"`
			Forced   int `json:"forced"`
			Attached int `json:"attached_pic"`
		} `json:"disposition"`
		Tags map[string]string `json:"tags"`
	} `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

// ffprobe repeats itself per unreadable stream; the first line is the cause.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

type Prober struct {
	ffprobe string
}

func NewProber(ffprobe string) *Prober { return &Prober{ffprobe: ffprobe} }

// A slow swarm can take minutes over the header. A var for tests.
var probeLimit = 3 * time.Minute

// ErrProbeTimeout: the start of the file had not arrived when the probe gave up.
var ErrProbeTimeout = errors.New("the start of the file didn't arrive")

// Probe inspects the source over HTTP, so the engine fetches only the header.
func (p *Prober) Probe(ctx context.Context, url string) (*MediaInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, probeLimit)
	defer cancel()

	// -v error rather than quiet: diagnostics go to stderr so stdout JSON stays
	// clean, and a failure can still say why instead of a bare "exit status 1".
	cmd := proc.Hide(exec.CommandContext(ctx, p.ffprobe,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-show_chapters",
		// Everything read below is in the headers; twenty seconds' worth meant
		// tens of megabytes, a minute through the engine on a fresh swarm.
		"-analyzeduration", "2M",
		"-probesize", "8M",
		url,
	))

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		if reason := strings.TrimSpace(stderr.String()); reason != "" {
			return nil, fmt.Errorf("ffprobe: %w: %s", err, firstLine(reason))
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ffprobe: %w within %s", ErrProbeTimeout, probeLimit)
		}
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}

	info := &MediaInfo{Container: raw.Format.FormatName}
	info.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	info.Bitrate, _ = strconv.ParseInt(raw.Format.BitRate, 10, 64)

	var mediaEnd float64
	// A track that does not say how long it is could be the one that runs longest.
	lengthsKnown := true
	media := func(duration string, tags map[string]string) {
		n := streamSeconds(duration, tags)
		lengthsKnown = lengthsKnown && n > 0
		mediaEnd = max(mediaEnd, n)
	}
	for _, s := range raw.Streams {
		stream := Stream{
			Index:    s.Index,
			Kind:     s.CodecType,
			Codec:    s.CodecName,
			Profile:  s.Profile,
			Width:    s.Width,
			Height:   s.Height,
			Channels: s.Channels,
			Language: s.Tags["language"],
			Title:    s.Tags["title"],
			Default:  s.Disposition.Default == 1,
			Forced:   s.Disposition.Forced == 1,
			BitDepth: bitDepth(s.PixFmt, s.BitsPerRaw),
		}
		if s.CodecType == "video" {
			stream.Transfer, stream.FieldOrder = s.Transfer, s.FieldOrder
			for _, d := range s.SideData {
				if d.DVProfile > 0 {
					stream.DVProfile = d.DVProfile
				}
			}
		}

		switch s.CodecType {
		case "video":
			// Cover art is stored as a video stream; it is not the picture.
			if s.Disposition.Attached == 1 {
				continue
			}
			if info.Video == nil {
				info.Video = &stream
			}
			media(s.Duration, s.Tags)
		case "audio":
			info.Audio = append(info.Audio, stream)
			media(s.Duration, s.Tags)
		case "subtitle":
			info.Subtitles = append(info.Subtitles, stream)
		case "attachment":
			info.Attachments = append(info.Attachments, Attachment{
				Index:    s.Index,
				Filename: s.Tags["filename"],
				MimeType: s.Tags["mimetype"],
			})
		}
	}

	for _, c := range raw.Chapters {
		start, _ := strconv.ParseFloat(c.StartTime, 64)
		end, _ := strconv.ParseFloat(c.EndTime, 64)
		if end <= start {
			continue
		}
		title := c.Tags["title"]
		kind := chapterKind(title)
		// Outside its kind's usual length, it's a mislabelled body.
		if lo, hi := plausibleSpan(kind); end-start < lo || end-start > hi {
			kind = ""
		}
		info.Chapters = append(info.Chapters, Chapter{
			Title: title, Start: start, End: end, Kind: kind,
		})
	}

	if info.Video == nil {
		return nil, fmt.Errorf("no video stream in %s", url)
	}
	// The playlist ends with the picture and sound: subtitles running past them left slots with nothing in.
	// Only by a little: a stale length tag from an older cut of the file mustn't cut real minutes off the end.
	if lengthsKnown && mediaEnd > 0 && mediaEnd < info.Duration && info.Duration-mediaEnd <= maxSubtitleOverhang {
		info.Duration = mediaEnd
	}
	info.Audio = dropMirror(info.Audio)
	info.Subtitles = dropMirror(info.Subtitles)
	return info, nil
}

// How far subtitles may run past the picture and sound before the tracks' own lengths are doubted.
const maxSubtitleOverhang = 30.0

// streamSeconds is one track's length: ffprobe's, or the DURATION tag Matroska muxers write; 0 when neither.
func streamSeconds(duration string, tags map[string]string) float64 {
	if v, err := strconv.ParseFloat(duration, 64); err == nil && v > 0 {
		return v
	}
	// In sorted order, the plain DURATION before a DURATION-eng: a map's own order changes run to run.
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		var h, m int
		var s float64
		if strings.HasPrefix(strings.ToUpper(k), "DURATION") {
			if n, _ := fmt.Sscanf(tags[k], "%d:%d:%f", &h, &m, &s); n == 3 {
				return float64(h*3600+m*60) + s
			}
		}
	}
	return 0
}

// dropMirror removes the phantom second copy of the tracks ffprobe reports mid-download.
func dropMirror(tracks []Stream) []Stream {
	n := len(tracks)
	if n < 2 || n%2 != 0 {
		return tracks
	}
	half := n / 2
	for i := range half {
		a, b := tracks[i], tracks[half+i]
		if b.Index <= a.Index || a.Kind != b.Kind || a.Codec != b.Codec || a.Language != b.Language ||
			a.Title != b.Title || a.Channels != b.Channels || a.Default != b.Default || a.Forced != b.Forced {
			return tracks
		}
	}
	return tracks[:half]
}

func bitDepth(pixFmt, bitsPerRaw string) int {
	if n, err := strconv.Atoi(bitsPerRaw); err == nil && n > 0 {
		return n
	}
	switch {
	case strings.Contains(pixFmt, "p10"):
		return 10
	case strings.Contains(pixFmt, "p12"):
		return 12
	case pixFmt != "":
		return 8
	}
	return 0
}

// Plan decides what has to be re-encoded. Re-encoding costs GPU time and
// quality, so it is only done when the browser cannot play the stream.
type Plan struct {
	VideoCopy  bool   `json:"videoCopy"`
	AudioCopy  bool   `json:"audioCopy"`
	VideoCodec string `json:"videoCodec"`
	AudioCodec string `json:"audioCodec"`
	// Channels: a converted track's channel count, 2 or 6; 0 keeps the source's (FLAC up to 5.1).
	Channels int `json:"channels,omitempty"`
	// ForDevice: copies what any browser's plan converts, as this device said it decodes it.
	ForDevice bool `json:"forDevice,omitempty"`
	// Picture changes, each forcing an encode: tone map, deinterlace, soften, a cap (height, bits/s).
	ToneMap     bool `json:"toneMap,omitempty"`
	Deinterlace bool `json:"deinterlace,omitempty"`
	Soften      bool `json:"soften,omitempty"`
	Height      int  `json:"height,omitempty"`
	MaxRate     int  `json:"maxRate,omitempty"`
	// Sound is what is done to the audio on the way out; anything at all forces an audio re-encode.
	Sound  sound.Options `json:"sound"`
	Reason string        `json:"reason"`
}

// Chromium decodes H.264 and AAC everywhere; HEVC needs a platform extension
// and 10-bit H.264 has no hardware decoder, so neither can be relied on.
func PlanFor(info *MediaInfo, encoder string) Plan {
	return planFor(info, encoder, 0, sound.Options{}, nil)
}

// planOf: a session's plan; drawn-in subtitles change the picture, so it is always encoded.
func planOf(info *MediaInfo, encoder string, track int, snd sound.Options, caps *Caps, want Want, burned bool) Plan {
	plan := planWith(info, encoder, track, snd, caps, want)
	if burned {
		plan.VideoCopy = false
		plan.VideoCodec = orDefault(encoder, "libx264")
		plan.Reason = strings.TrimPrefix(plan.Reason+"; subtitles drawn in", "; ")
	}
	return plan
}

// plannable is info as planned with this ffmpeg's tone map; with none (or none found yet) HDR goes as it is,
// as an encode would look no better than the copy.
func plannable(info *MediaInfo, toneMapper string) *MediaInfo {
	if toneMapper != "" || info.Video == nil || !info.Video.HDR() {
		return info
	}
	shown, v := *info, *info.Video
	v.Transfer, v.DVProfile = "", 0
	shown.Video = &v
	return &shown
}

// planFor judges the track that will be sent, under the sound asked, for a device's caps or any browser (nil).
func planFor(info *MediaInfo, encoder string, track int, snd sound.Options, caps *Caps) Plan {
	return planWith(info, encoder, track, snd, caps, Want{})
}

// planWith is planFor under what the viewer wants of the picture.
func planWith(info *MediaInfo, encoder string, track int, snd sound.Options, caps *Caps, want Want) Plan {
	plan := Plan{VideoCopy: true, AudioCopy: true, Sound: sound.Normalized(snd)}
	var reasons []string

	switch v := info.Video; {
	case v == nil:
	case caps != nil:
		plan.VideoCopy = caps.Decodes(v.Codec, v.BitDepth)
	case v.Codec == "h264" && v.BitDepth <= 8:
	case v.Codec == "vp9", v.Codec == "av1":
		// Chromium decodes both natively.
	default:
		plan.VideoCopy = false
	}
	if !plan.VideoCopy {
		reasons = append(reasons, fmt.Sprintf("video %s %d-bit", info.Video.Codec, info.Video.BitDepth))
	}
	if v := info.Video; v != nil {
		plan.Deinterlace = v.Interlaced()
		plan.Soften = want.Soften
		// A picture already under the cap is sent as it is.
		if want.Height > 0 && (v.Height > want.Height || info.Bitrate > int64(want.Rate)*5/4) {
			plan.Height, plan.MaxRate = min(v.Height, want.Height), want.Rate
		}
		// HDR stays only uncoded to an HDR screen; Dolby Vision 5 (no HDR10 under it) shows green and purple.
		encoded := !plan.VideoCopy || plan.Deinterlace || plan.Soften || plan.MaxRate > 0
		plan.ToneMap = v.HDR() && (encoded || caps == nil || !caps.HDR) || v.DVProfile == 5
		for _, change := range []struct {
			on   bool
			what string
		}{
			{plan.ToneMap, "HDR for this screen"}, {plan.Deinterlace, "interlacing"}, {plan.Soften, "softened flashing"},
			{plan.MaxRate > 0, fmt.Sprintf("a %dp cap", plan.Height)},
		} {
			if change.on {
				plan.VideoCopy = false
				reasons = append(reasons, change.what)
			}
		}
	}
	if !plan.VideoCopy {
		plan.VideoCodec = orDefault(encoder, "libx264")
	}

	if track < 0 || track >= len(info.Audio) {
		track = 0
	}
	if len(info.Audio) > 0 {
		a := info.Audio[track]
		if caps != nil {
			plan.AudioCopy = caps.Decodes(a.Codec, 0)
		} else {
			// Not Vorbis: browsers take it only in WebM, and these segments are fMP4.
			switch a.Codec {
			case "aac", "mp3", "opus", "flac":
			default:
				plan.AudioCopy = false
			}
		}
		// Under a cap, lossless sound would outweigh the picture.
		if want.Rate > 0 && (a.Codec == "flac" || lossless(a)) {
			plan.AudioCopy = false
		}
		if !plan.AudioCopy {
			reasons = append(reasons, "audio "+a.Codec)
		}
		// The sound is shaped while encoding, so a track that could have been copied is encoded too.
		if plan.Sound.Any() {
			plan.AudioCopy = false
			reasons = append(reasons, "sound")
		}
		if !plan.AudioCopy {
			plan.AudioCodec = "aac"
			// Lossless stays lossless as FLAC, where the device plays it (a device may say it doesn't).
			if lossless(a) && want.Rate == 0 && (caps == nil || caps.Decodes("flac", 0)) {
				plan.AudioCodec = "flac"
			}
			plan.Channels = channelsOut(a, plan.AudioCodec, plan.Sound.Any() || want.Rate > 0, caps)
		}
	}

	if len(reasons) > 0 {
		plan.Reason = "re-encoding " + strings.Join(reasons, " and ")
	} else {
		plan.Reason = "remux only"
	}
	if caps != nil {
		anyBrowser := planFor(info, encoder, track, snd, nil)
		plan.ForDevice = plan.VideoCopy && !anyBrowser.VideoCopy || plan.AudioCopy && !anyBrowser.AudioCopy
	}
	return plan
}

// channelsOut: 5.1 for surround speakers, else stereo (as shaped sound is); lossless keeps its own if unsaid.
func channelsOut(a Stream, codec string, shaped bool, caps *Caps) int {
	switch {
	case shaped:
		return 2
	case caps != nil && caps.Channels >= 6 && a.Channels > 2:
		return 6
	case codec == "flac" && (caps == nil || caps.Channels == 0):
		return 0
	}
	return 2
}

// lossless reports an audio track that holds the sound exactly, which a lossy re-encode would throw away.
func lossless(a Stream) bool {
	switch a.Codec {
	case "truehd", "mlp", "alac", "wavpack", "tta", "ape":
		return true
	case "dts":
		// The core is lossy; only the Master Audio extension is not.
		return strings.Contains(strings.ToUpper(a.Profile), "HD MA")
	}
	return strings.HasPrefix(a.Codec, "pcm_")
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
