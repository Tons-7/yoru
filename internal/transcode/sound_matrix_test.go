package transcode

import (
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"yoru/internal/sound"
)

// Every sound setting on every channel count encodes in full, to FLAC or AAC, from TrueHD's 40-sample frames.
func TestEverySoundOptionEncodes(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	clips := map[int]string{}
	for channels, layout := range map[int]string{1: "mono", 2: "stereo", 6: "5.1", 8: "7.1"} {
		clip := filepath.Join(t.TempDir(), fmt.Sprintf("%dch.mka", channels))
		var labels strings.Builder
		for i := range channels {
			fmt.Fprintf(&labels, "[c%d]", i)
		}
		build := fmt.Sprintf("[0]asplit=%d%s;%samerge=inputs=%d,aformat=channel_layouts=%s,asetnsamples=n=40[s]",
			channels, labels.String(), labels.String(), channels, layout)
		if channels == 1 {
			build = "[0]aformat=channel_layouts=mono,asetnsamples=n=40[s]"
		}
		if out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=2:sample_rate=48000",
			"-filter_complex", build, "-map", "[s]", "-c:a", "pcm_s24le", clip).CombinedOutput(); err != nil {
			t.Fatalf("build %d-channel clip: %v\n%s", channels, err, out)
		}
		clips[channels] = clip
	}

	var settings []sound.Options
	for _, voice := range []string{"off", "medium", "high"} {
		for _, boost := range []string{"off", "medium", "high"} {
			for bits := range 8 {
				settings = append(settings, sound.Options{Voice: voice, Boost: boost,
					Night: bits&1 != 0, Headphones: bits&2 != 0, Mono: bits&4 != 0})
			}
		}
	}

	for _, channels := range []int{1, 2, 6, 8} {
		for _, codec := range []string{"flac", "aac"} {
			t.Run(fmt.Sprintf("%dch-%s", channels, codec), func(t *testing.T) {
				t.Parallel()
				source := "truehd"
				if codec == "aac" {
					source = "eac3"
				}
				for _, opts := range settings {
					s := liveSession(false)
					s.Info.Audio = []Stream{{Codec: source, Channels: channels}}
					s.sound = sound.Normalized(opts)
					s.Plan = planFor(s.Info, s.encoder, 0, s.sound)
					file := filepath.Join(t.TempDir(), "out.mka")
					args := append([]string{"-hide_banner", "-loglevel", "error", "-y", "-i", clips[channels], "-map", "0:a"},
						s.audioArgs()...)
					if out, err := exec.Command(ffmpeg, append(args, file)...).CombinedOutput(); err != nil {
						first, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
						t.Fatalf("%+v: %v: %s", opts, err, first)
					}
					got, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_name:format=duration",
						"-of", "default=nw=1:nk=1", file).Output()
					if err != nil {
						t.Fatalf("%+v: probe: %v", opts, err)
					}
					fields := strings.Fields(string(got))
					if len(fields) < 2 || fields[0] != codec {
						t.Fatalf("%+v: came out as %q, want %s", opts, got, codec)
					}
					if d, _ := strconv.ParseFloat(fields[len(fields)-1], 64); math.Abs(d-2) > 0.1 {
						t.Errorf("%+v: %.3fs long, want 2s", opts, d)
					}
				}
			})
		}
	}
}
