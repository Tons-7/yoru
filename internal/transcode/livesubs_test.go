package transcode

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yoru/internal/sound"
)

func liveSession(on bool) *Session {
	return &Session{
		Source: "http://x/1", liveSubs: on,
		Plan: Plan{VideoCopy: true, AudioCopy: true},
		Info: &MediaInfo{
			Video: &Stream{Codec: "h264", BitDepth: 8},
			Subtitles: []Stream{
				{Index: 2, Codec: "ass"}, {Index: 3, Codec: "subrip"}, {Index: 4, Codec: "hdmv_pgs_subtitle"},
			},
		},
	}
}

func TestSubtitleProcessWritesEveryTextTrack(t *testing.T) {
	args := strings.Join(liveSession(true).subtitleArgs(60, 10), " ")
	for _, want := range []string{
		"-map 0:2? -c:s copy -f ass -ignore_readorder 1 -flush_packets 1 -y live-2-10.ass",
		"-map 0:3? -c:s ass -f ass -ignore_readorder 1 -flush_packets 1 -y live-3-10.ass",
		"-ss 60.000", "-readrate 2.0", "-i http://x/1", "-copyts",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in: %s", want, args)
		}
	}
	if strings.Contains(args, "live-4-") {
		t.Errorf("a bitmap track cannot be written as text: %s", args)
	}
	if strings.Contains(args, "playlist.m3u8") || strings.Contains(args, "-c:v") {
		t.Errorf("the subtitle process must not write the picture: %s", args)
	}
}

func TestPassCarriesNoSubtitleOutputs(t *testing.T) {
	for _, plan := range []Plan{{VideoCopy: true, AudioCopy: true}, {VideoCopy: true}} {
		s := liveSession(true)
		s.Plan = plan
		if args := strings.Join(s.args(&run{from: 10}), " "); strings.Contains(args, "live-") || strings.Contains(args, "-c:s") {
			t.Errorf("subtitle output on the pass (audio copied: %v): %s", plan.AudioCopy, args)
		}
	}
}

func TestNoSubtitleProcessWithoutTextTracksOrWhenOff(t *testing.T) {
	if args := liveSession(false).subtitleArgs(0, 0); args != nil {
		t.Errorf("a subtitle process though switched off: %v", args)
	}
	bitmapOnly := liveSession(true)
	bitmapOnly.Info.Subtitles = []Stream{{Index: 4, Codec: "hdmv_pgs_subtitle"}}
	if args := bitmapOnly.subtitleArgs(0, 0); args != nil {
		t.Errorf("a subtitle process with nothing it can write: %v", args)
	}
}

func TestAwaitLiveServesTheTrackBeforeItsFirstLine(t *testing.T) {
	dir := t.TempDir()
	s := NewSubtitles("ffmpeg")
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.WriteFile(filepath.Join(dir, "live-2-0.ass"), []byte(assHead), 0o644)
	}()

	began := time.Now()
	path, ok := s.AwaitLive(context.Background(), dir, 2, "ass")
	if !ok || time.Since(began) > 3*time.Second {
		t.Fatalf("ok %v after %s, want the header-only track within moments", ok, time.Since(began).Round(time.Millisecond))
	}
	if text, _ := os.ReadFile(path); !strings.Contains(string(text), "[Events]") || strings.Contains(string(text), "Dialogue:") {
		t.Errorf("served %q, want the header and no lines", text)
	}
}

func TestAwaitLiveGivesUpWhenNothingIsWritten(t *testing.T) {
	old := liveHeaderWait
	liveHeaderWait = 400 * time.Millisecond
	defer func() { liveHeaderWait = old }()

	began := time.Now()
	if _, ok := NewSubtitles("ffmpeg").AwaitLive(context.Background(), t.TempDir(), 2, "ass"); ok {
		t.Fatal("reported a track that nothing wrote")
	}
	if waited := time.Since(began); waited > 2*time.Second {
		t.Errorf("gave up only after %s", waited)
	}
}

func TestLiveSubtitlesNeedsTheSubtitleProcess(t *testing.T) {
	s := liveSession(true)
	s.run = &run{}
	if _, ok := s.LiveSubtitles(); ok {
		t.Error("reported live lines with no subtitle process")
	}
	s.run.subsAlive.Store(true)
	if _, ok := s.LiveSubtitles(); !ok {
		t.Error("a running subtitle process was not reported")
	}
}

const assHead = "[Script Info]\nTitle: x\n\n[V4+ Styles]\nFormat: Name\nStyle: Default\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"

func cue(start, text string) string {
	return "Dialogue: 0," + start + ",0:59:59.00,Default,,0,0,0,," + text + "\n"
}

func TestMergeLiveUnionsEveryPassIntoTheTrack(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "sub-2.ass")
	os.WriteFile(track, []byte(assHead+cue("0:00:01.00", "one")+cue("0:00:05.00", "five")), 0o644)
	os.WriteFile(filepath.Join(dir, "live-2-0.ass"), []byte(assHead+cue("0:00:05.00", "five")+cue("0:00:09.00", "nine")), 0o644)
	os.WriteFile(filepath.Join(dir, "live-2-50.ass"), []byte(assHead+cue("0:05:02.00", "later")+"Dialogue: 0,0:05:09.00,0:05:1"), 0o644)
	os.WriteFile(filepath.Join(dir, "live-3-0.ass"), []byte(assHead+cue("0:00:02.00", "other track")), 0o644)

	subs := NewSubtitles("ffmpeg")
	path, err := subs.MergeLive(dir, 2, "ass")
	if err != nil || path != track {
		t.Fatalf("path %q err %v", path, err)
	}
	got, _ := os.ReadFile(track)
	text := string(got)
	for _, want := range []string{"one", "five", "nine", "later"} {
		if strings.Count(text, ",,"+want+"\n") != 1 {
			t.Errorf("%q appears %d times, want once:\n%s", want, strings.Count(text, ",,"+want+"\n"), text)
		}
	}
	if strings.Contains(text, "other track") || strings.Contains(text, "0:05:09.00") {
		t.Errorf("merged a line it should not have:\n%s", text)
	}
	if n := subs.Cues(dir, 2, "ass"); n != 4 {
		t.Errorf("%d cues, want 4", n)
	}
	if strings.Index(text, "nine") > strings.Index(text, "later") {
		t.Errorf("lines out of time order:\n%s", text)
	}
}

func TestMergeLiveAloneBecomesTheTrack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "live-2-0.ass"), []byte(assHead+cue("0:00:03.00", "first")), 0o644)

	subs := NewSubtitles("ffmpeg")
	path, err := subs.MergeLive(dir, 2, "ass")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "Style: Default") || subs.Cues(dir, 2, "ass") != 1 {
		t.Fatalf("track = %q", got)
	}
}

func TestMergeLiveWithNothingNewLeavesTheFileAlone(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "sub-2.ass")
	os.WriteFile(track, []byte(assHead+cue("0:00:01.00", "one")), 0o644)
	before, _ := os.Stat(track)

	if _, err := NewSubtitles("ffmpeg").MergeLive(dir, 2, "ass"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(track)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("rewrote a track with nothing to add")
	}
}

func TestLiveCoversOnlyPastWhereThePassBegan(t *testing.T) {
	for _, c := range []struct {
		start, at float64
		want      bool
	}{
		{0, 5, true},
		{300, 305, false},
		{300, 340, true},
	} {
		if got := LiveCovers(c.start, c.at); got != c.want {
			t.Errorf("LiveCovers(%v, %v) = %v", c.start, c.at, got)
		}
	}
}

func lastPacket(t *testing.T, ffprobe string, session *Session, segment int, stream string) float64 {
	t.Helper()
	init, err := os.ReadFile(session.InitPath())
	if err != nil {
		t.Fatal(err)
	}
	body := segmentBytes(t, session, segment)
	whole := filepath.Join(t.TempDir(), "whole.mp4")
	if err := os.WriteFile(whole, append(init, body...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", stream,
		"-show_entries", "packet=pts_time", "-of", "csv=p=0", whole).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	last := -1.0
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(line, ",")), 64); err == nil && v > last {
			last = v
		}
	}
	return last
}

func TestReEncodedAudioKeepsUpWithThePictureWhileSubtitlesAreWritten(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	dir := t.TempDir()
	subs := filepath.Join(dir, "subs.ass")
	if err := os.WriteFile(subs, []byte(testASS+"Dialogue: 0,0:00:50.00,0:00:55.00,Default,tail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(dir, "clip.mkv")
	if o, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=60",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=60",
		"-i", subs, "-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "48",
		"-c:a", "eac3", "-c:s", "copy", clip).CombinedOutput(); err != nil {
		t.Fatalf("build clip: %v\n%s", err, o)
	}

	log := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", log)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := m.Open(ctx, "sync", clip)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close("sync")
	if plan := session.CurrentPlan(); !plan.VideoCopy || plan.AudioCopy {
		t.Fatalf("plan = %+v, want the picture copied and the sound re-encoded", plan)
	}
	session.mu.Lock()
	session.liveSubs = true
	session.mu.Unlock()

	for n := range 4 {
		if err := session.WaitSegment(ctx, n, 90*time.Second); err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
	}
	for n := range 3 {
		video, audio := lastPacket(t, ffprobe, session, n, "v:0"), lastPacket(t, ffprobe, session, n, "a:0")
		if audio < video-1 {
			t.Errorf("segment %d: picture runs to %.2fs but sound only to %.2fs", n, video, audio)
		}
	}

	live := filepath.Join(filepath.Dir(session.InitPath()), liveName(session.Info.Subtitles[0].Index, 0))
	deadline := time.Now().Add(20 * time.Second)
	for {
		if text, _ := os.ReadFile(live); strings.Contains(string(text), "styled line one") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no subtitle lines in %s", live)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestLosslessAudioIsConvertedToFLACAndLossyToAAC(t *testing.T) {
	for _, tt := range []struct {
		audio Stream
		want  string
	}{
		{Stream{Codec: "truehd", Channels: 8}, "flac"},
		{Stream{Codec: "pcm_s24le", Channels: 2}, "flac"},
		{Stream{Codec: "dts", Profile: "DTS-HD MA", Channels: 6}, "flac"},
		{Stream{Codec: "dts", Profile: "DTS", Channels: 6}, "aac"},
		{Stream{Codec: "eac3", Channels: 6}, "aac"},
		{Stream{Codec: "ac3", Channels: 6}, "aac"},
	} {
		info := &MediaInfo{Video: &Stream{Codec: "h264", BitDepth: 8}, Audio: []Stream{tt.audio}}
		plan := PlanFor(info, "")
		if plan.AudioCopy || plan.AudioCodec != tt.want {
			t.Errorf("%s %s: copy %v, codec %q, want %q", tt.audio.Codec, tt.audio.Profile, plan.AudioCopy, plan.AudioCodec, tt.want)
		}
	}
	if plan := PlanFor(&MediaInfo{Video: &Stream{Codec: "h264", BitDepth: 8}, Audio: []Stream{{Codec: "flac"}}}, ""); !plan.AudioCopy {
		t.Error("a FLAC track is re-encoded")
	}
}

func TestFLACConversionKeepsSurround(t *testing.T) {
	args := func(channels int) string {
		s := liveSession(false)
		s.Info.Audio = []Stream{{Codec: "truehd", Channels: channels}}
		s.Plan = PlanFor(s.Info, "")
		return strings.Join(s.audioArgs(), " ")
	}
	if got := args(6); got != "-c:a flac" {
		t.Errorf("5.1: %q, want the channels left alone", got)
	}
	if got := args(8); got != "-ac 6 -c:a flac" {
		t.Errorf("7.1: %q, want it folded to 5.1", got)
	}
}

func TestLosslessSurroundPlaysWithSoundOptions(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	clip := filepath.Join(t.TempDir(), "clip.mkv")
	if o, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30:sample_rate=48000",
		"-filter_complex", "[1]asplit=6[a][b][c][d][e][f];[a][b][c][d][e][f]amerge=inputs=6,aformat=channel_layouts=5.1,asetnsamples=n=40[s]",
		"-map", "0:v", "-map", "[s]",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "48",
		"-c:a", "pcm_s24le", clip).CombinedOutput(); err != nil {
		t.Fatalf("build clip: %v\n%s", err, o)
	}

	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := m.Open(ctx, "flac-sound", clip)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close("flac-sound")
	session.SetSound(sound.Options{Voice: "high", Headphones: true})
	if plan := session.CurrentPlan(); plan.AudioCodec != "flac" {
		t.Fatalf("plan = %+v, want FLAC", plan)
	}
	for n := range 2 {
		if err := session.WaitSegment(ctx, n, 90*time.Second); err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
	}
	init, _ := os.ReadFile(session.InitPath())
	body := segmentBytes(t, session, 0)
	whole := filepath.Join(t.TempDir(), "whole.mp4")
	os.WriteFile(whole, append(init, body...), 0o644)
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name,channels", "-of", "csv=p=0", whole).Output()
	if err != nil || strings.TrimSpace(string(out)) != "flac,2" {
		t.Fatalf("segment audio is %q (err %v), want stereo FLAC", strings.TrimSpace(string(out)), err)
	}
}

func TestLosslessSourcePlaysAsFLAC(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	clip := filepath.Join(t.TempDir(), "clip.mkv")
	if o, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=40",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=40",
		"-map", "0:v", "-map", "1:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "48",
		"-c:a", "pcm_s24le", "-ac", "2", clip).CombinedOutput(); err != nil {
		t.Fatalf("build clip: %v\n%s", err, o)
	}

	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := m.Open(ctx, "flac", clip)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close("flac")
	if plan := session.CurrentPlan(); plan.AudioCopy || plan.AudioCodec != "flac" {
		t.Fatalf("plan = %+v", plan)
	}
	for n := range 3 {
		if err := session.WaitSegment(ctx, n, 90*time.Second); err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
	}

	init, _ := os.ReadFile(session.InitPath())
	body := segmentBytes(t, session, 0)
	whole := filepath.Join(t.TempDir(), "whole.mp4")
	os.WriteFile(whole, append(init, body...), 0o644)
	codec, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", whole).Output()
	if err != nil || strings.TrimSpace(string(codec)) != "flac" {
		t.Fatalf("segment audio is %q (err %v), want flac", strings.TrimSpace(string(codec)), err)
	}
	for n := range 2 {
		video, audio := lastPacket(t, ffprobe, session, n, "v:0"), lastPacket(t, ffprobe, session, n, "a:0")
		if audio < video-1 {
			t.Errorf("segment %d: picture runs to %.2fs but sound only to %.2fs", n, video, audio)
		}
	}
}
