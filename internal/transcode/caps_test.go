package transcode

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"yoru/internal/sound"
)

func TestPlanCopiesWhatTheDeviceDecodes(t *testing.T) {
	hevc := &MediaInfo{Video: &Stream{Codec: "hevc", BitDepth: 10}, Audio: []Stream{{Codec: "eac3", Channels: 6}}}
	for _, tt := range []struct {
		caps              string
		videoCopy, device bool
		audio             string
	}{
		{"", false, false, "aac"},
		{"hevc", false, false, "aac"},
		{"hevc-10", true, true, "aac"},
		{"hevc-10,eac3", true, true, ""},
		{"eac3", false, true, ""},
	} {
		p := planFor(hevc, "h264_nvenc", 0, sound.Options{}, ParseCaps(tt.caps))
		if p.VideoCopy != tt.videoCopy || p.ForDevice != tt.device || p.AudioCodec != tt.audio {
			t.Errorf("caps %q: copy %v, for device %v, audio %q; want %v %v %q", tt.caps, p.VideoCopy, p.ForDevice,
				p.AudioCodec, tt.videoCopy, tt.device, tt.audio)
		}
	}

	av1 := &MediaInfo{Video: &Stream{Codec: "av1", BitDepth: 10}, Audio: []Stream{{Codec: "flac", Channels: 2}}}
	if p := planFor(av1, "", 0, sound.Options{}, nil); !p.VideoCopy || !p.AudioCopy || p.ForDevice {
		t.Errorf("any browser: %+v, want both copied as before", p)
	}
	if p := planFor(av1, "", 0, sound.Options{}, ParseCaps("ch2")); p.VideoCopy || p.AudioCopy {
		t.Errorf("a device without AV1 or FLAC: %+v, want both converted", p)
	}
	if p := planFor(av1, "", 0, sound.Options{}, ParseCaps("av1-10,flac")); !p.VideoCopy || !p.AudioCopy || p.ForDevice {
		t.Errorf("a device with both: %+v", p)
	}
	truehd := &MediaInfo{Video: &Stream{Codec: "h264", BitDepth: 8}, Audio: []Stream{{Codec: "truehd", Channels: 6}}}
	if p := planFor(truehd, "", 0, sound.Options{}, ParseCaps("ch2")); p.AudioCodec != "aac" {
		t.Errorf("no FLAC here: %s", p.AudioCodec)
	}
	if p := planFor(truehd, "", 0, sound.Options{}, ParseCaps("flac,ch2")); p.AudioCodec != "flac" {
		t.Errorf("FLAC here: %s", p.AudioCodec)
	}
	h264 := &MediaInfo{Video: &Stream{Codec: "h264", BitDepth: 8}, Audio: []Stream{{Codec: "aac"}}}
	if p := planFor(h264, "", 0, sound.Options{}, ParseCaps("ch2")); !p.VideoCopy || !p.AudioCopy || p.ForDevice {
		t.Errorf("h264 and aac: %+v", p)
	}
}

func TestChannelsFollowTheDevice(t *testing.T) {
	ac3 := Stream{Codec: "ac3", Channels: 6}
	truehd := Stream{Codec: "truehd", Channels: 8}
	for _, tt := range []struct {
		track Stream
		caps  string
		snd   sound.Options
		want  int
		args  string
	}{
		{ac3, "", sound.Options{}, 2, "-af " + sound.Stereo + " -c:a aac -b:a 192k"},
		{ac3, "ch2", sound.Options{}, 2, "-af " + sound.Stereo + " -c:a aac -b:a 192k"},
		{ac3, "ch6", sound.Options{}, 6, "-af aformat=channel_layouts=5.1 -c:a aac -b:a 384k"},
		{ac3, "ch8", sound.Options{Night: true}, 2, ""},
		{Stream{Codec: "dts", Channels: 8}, "ch8", sound.Options{}, 6, "-af aformat=channel_layouts=5.1 -c:a aac -b:a 384k"},
		{Stream{Codec: "ac3", Channels: 1}, "ch6", sound.Options{}, 2, "-ac 2 -c:a aac -b:a 192k"},
		{truehd, "", sound.Options{}, 0, "-ac 6 -c:a flac"},
		{truehd, "flac,ch2", sound.Options{}, 2, "-af " + sound.Stereo + " -c:a flac"},
		{truehd, "ch2", sound.Options{}, 2, "-af " + sound.Stereo + " -c:a aac -b:a 192k"},
		{Stream{Codec: "truehd", Channels: 6}, "", sound.Options{}, 0, "-c:a flac"},
	} {
		info := &MediaInfo{Video: &Stream{Codec: "h264", BitDepth: 8}, Audio: []Stream{tt.track}}
		s := &Session{Info: info, sound: tt.snd, caps: ParseCaps(tt.caps)}
		s.replan()
		if s.Plan.Channels != tt.want {
			t.Errorf("%s %dch to %q: %d channels, want %d", tt.track.Codec, tt.track.Channels, tt.caps, s.Plan.Channels, tt.want)
		}
		if got := strings.Join(s.audioArgs(), " "); tt.args != "" && got != tt.args {
			t.Errorf("%s %dch to %q: %s\nwant %s", tt.track.Codec, tt.track.Channels, tt.caps, got, tt.args)
		}
	}
}

func TestVariantsArePerPlan(t *testing.T) {
	hevc := &MediaInfo{Video: &Stream{Codec: "hevc", BitDepth: 10}, Audio: []Stream{{Codec: "aac", Channels: 2}}}
	h264 := &MediaInfo{Video: &Stream{Codec: "h264", BitDepth: 8}, Audio: []Stream{{Codec: "aac", Channels: 2}}}

	if id := variantID("5-1", hevc, "", nil, Want{}); id != "5-1" {
		t.Errorf("no caps: %s", id)
	}
	if id := variantID("5-1", h264, "", ParseCaps("hevc-10,ch2"), Want{}); id != "5-1" {
		t.Errorf("an h264 file plays alike everywhere: %s", id)
	}
	a, b := variantID("5-1", hevc, "", ParseCaps("hevc-10"), Want{}), variantID("5-1", hevc, "", ParseCaps("hevc-10,opus,ch2"), Want{})
	if a == "5-1" || a != b {
		t.Errorf("same plan, same variant: %s %s", a, b)
	}
	if BaseID(a) != "5-1" || BaseID(a+"-burn2-0") != "5-1" || BaseID("-55-7-burn0-5") != "-55-7" || BaseID("5-1") != "5-1" {
		t.Errorf("BaseID: %s", BaseID(a))
	}
	keep := variantID("5-1", h264, "", ParseCaps("hevc-10,ch2"), Want{Keep: true})
	if kept := variantID("5-1", hevc, "", ParseCaps("hevc-10"), Want{Keep: true}); keep != "5-1-keep" || kept != a+"-keep" || BaseID(kept) != "5-1" {
		t.Errorf("kept copies: %s %s", keep, kept)
	}
}

func TestHDRConvertsOnlyWithAToneMap(t *testing.T) {
	hdr := &MediaInfo{Video: &Stream{Codec: "hevc", BitDepth: 10, Transfer: "smpte2084"}, Audio: []Stream{{Codec: "aac", Channels: 2}}}
	caps := ParseCaps("hevc-10")
	if p := planOf(plannable(hdr, "libplacebo"), "", 0, sound.Options{}, caps, Want{}, false); p.VideoCopy || !p.ToneMap {
		t.Errorf("with a tone map, to an SDR screen: %+v", p)
	}
	if p := planOf(plannable(hdr, ""), "", 0, sound.Options{}, caps, Want{}, false); !p.VideoCopy || p.ToneMap {
		t.Errorf("without one: %+v", p)
	}
	if !hdr.Video.HDR() {
		t.Error("planning without a tone map changed the file's own info")
	}
}

func TestEncoderOptions(t *testing.T) {
	tuned := map[string][]string{"h264_nvenc": {"-spatial-aq", "1"}}
	args := func(codec string) string {
		s := &Session{Plan: Plan{VideoCodec: codec}, encoder: codec, tuned: tuned}
		return strings.Join(s.videoArgs(), " ")
	}
	if got := args("h264_nvenc"); !strings.Contains(got, "-spatial-aq 1") {
		t.Errorf("nvenc: %s", got)
	}
	if got := args("libx264"); strings.Contains(got, "spatial-aq") || !strings.Contains(got, "-tune animation") {
		t.Errorf("libx264: %s", got)
	}
	s := &Session{Info: &MediaInfo{Video: &Stream{Codec: "hevc"}}, Plan: Plan{VideoCopy: true}}
	if got := s.videoArgs(); !slices.Equal(got, []string{"-c:v", "copy", "-tag:v", "hvc1"}) {
		t.Errorf("hevc copy: %v", got)
	}
}

func TestDeviceVariantSession(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	dir := t.TempDir()
	clip := filepath.Join(dir, "hevc.mkv")
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-c:v", "libx265", "-preset", "ultrafast", "-pix_fmt", "yuv420p10le", "-x265-params", "log-level=error",
		"-c:a", "ac3", "-ac", "6", clip).CombinedOutput(); err != nil {
		t.Skipf("no libx265 here: %v %s", err, out)
	}

	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	anyBrowser, err := m.OpenFor(ctx, "9-1", clip, clip, nil, Want{})
	if err != nil {
		t.Fatal(err)
	}
	device, err := m.OpenFor(ctx, "9-1", clip, clip, ParseCaps("hevc-10,ch2"), Want{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(anyBrowser.ID)
	defer m.Close(device.ID)

	if anyBrowser.ID != "9-1" || anyBrowser.Plan.VideoCopy {
		t.Errorf("any browser: %s %+v", anyBrowser.ID, anyBrowser.Plan)
	}
	if device.ID == "9-1" || !device.Plan.VideoCopy || !device.Plan.ForDevice || device.Info != anyBrowser.Info {
		t.Errorf("device: %s %+v, own probe %v", device.ID, device.Plan, device.Info != anyBrowser.Info)
	}
	if again, _ := m.OpenFor(ctx, "9-1", clip, clip, ParseCaps("hevc-10"), Want{}); again != device {
		t.Error("the same plan opened another session")
	} else {
		m.Close(again.ID)
	}
	if s, ok := m.Episode("9-1"); !ok || BaseID(s.ID) != "9-1" {
		t.Error("the episode's sessions aren't found by its id")
	}

	if err := device.WaitSegment(ctx, 0, time.Minute); err != nil {
		t.Fatal(err)
	}
	init, err := os.ReadFile(device.InitPath())
	if err != nil || !bytes.Contains(init, []byte("hvc1")) {
		t.Errorf("init is not tagged hvc1 (%v)", err)
	}
	seg, _ := os.ReadFile(device.SegmentPath(0))
	joined := filepath.Join(dir, "joined.mp4")
	if err := os.WriteFile(joined, append(init, seg...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_name,channels", "-of", "csv=p=0",
		joined).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "hevc") || !strings.Contains(string(out), "aac,2") {
		t.Errorf("segment 0: %s %v, want hevc copied and stereo AAC", out, err)
	}
}
