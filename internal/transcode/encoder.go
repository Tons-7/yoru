package transcode

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"yoru/internal/proc"
)

// Hardware encoders per system, fastest first, each tried here. Only H.264: every browser decodes it.
var hardwareEncoders = map[string][]string{
	"windows": {"h264_nvenc", "h264_qsv", "h264_amf"},
	"darwin":  {"h264_videotoolbox"},
	"linux":   {"h264_nvenc", "h264_qsv", "h264_vaapi"},
}[runtime.GOOS]

// VA-API encodes from a render node's surfaces, which frames are uploaded to after every filter.
const vaapiDevice = "/dev/dri/renderD128"

// IsHardwareEncoder: a GPU encoder, which transcodes HEVC/10-bit in real time.
func IsHardwareEncoder(codec string) bool { return usesHardware(codec) }

// DetectEncoder picks the first hardware encoder that really encodes here (all builds list nvenc), or libx264.
func DetectEncoder(ctx context.Context, ffmpeg string, log *slog.Logger) string {
	out, err := proc.Hide(exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-encoders")).Output()
	if err != nil {
		return softwareEncoder
	}
	available := string(out)

	for _, codec := range hardwareEncoders {
		if !strings.Contains(available, codec) {
			continue
		}
		if err := trialEncode(ctx, ffmpeg, codec); err != nil {
			log.Info("hardware encoder unusable here", "encoder", codec, "err", err)
			continue
		}
		return codec
	}
	return softwareEncoder
}

// CanEncode: ffmpeg has libx264 (Fedora's ffmpeg-free lacks it) or a working hardware encoder.
func CanEncode(ctx context.Context, ffmpeg string) bool {
	out, err := proc.Hide(exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-encoders")).Output()
	if err != nil {
		return false
	}
	available := string(out)
	if strings.Contains(available, " "+softwareEncoder+" ") {
		return true
	}
	for _, codec := range hardwareEncoders {
		if strings.Contains(available, codec) && trialEncode(ctx, ffmpeg, codec) == nil {
			return true
		}
	}
	return false
}

// Version is the version ffmpeg or ffprobe reports, or "".
func Version(ctx context.Context, binary string) string {
	out, err := proc.Hide(exec.CommandContext(ctx, binary, "-hide_banner", "-version")).Output()
	if err != nil {
		return ""
	}
	// "ffmpeg version 7.1-full_build-www.gyan.dev Copyright ..."
	fields := strings.Fields(string(out))
	if len(fields) >= 3 && fields[1] == "version" {
		return fields[2]
	}
	return ""
}

// Quality sets per GPU encoder, best first: AQ and lookahead against banding; no scene-cut keyframes.
var tunings = map[string][][]string{
	"h264_nvenc": {
		{"-spatial-aq", "1", "-temporal-aq", "1", "-rc-lookahead", "20", "-no-scenecut", "1"},
		{"-spatial-aq", "1", "-rc-lookahead", "20", "-no-scenecut", "1"},
	},
	"h264_qsv":          {{"-look_ahead", "1", "-look_ahead_depth", "20", "-adaptive_i", "0"}},
	"h264_videotoolbox": {{"-q:v", "65"}},
}

// Tuning is the first of the encoder's quality sets that encodes here (GPUs differ), or none.
func Tuning(ctx context.Context, ffmpeg, codec string) []string {
	for _, set := range tunings[codec] {
		if trialEncode(ctx, ffmpeg, codec, set...) == nil {
			return set
		}
	}
	return nil
}

// ToneMapper is the HDR tone map this ffmpeg runs: libplacebo's on the GPU (Vulkan), else zscale's, or "".
func ToneMapper(ctx context.Context, ffmpeg string) string {
	for _, name := range []string{"libplacebo", "zscale"} {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := proc.Hide(exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24,format=yuv420p10le,"+
				"setparams=color_trc=smpte2084:color_primaries=bt2020:colorspace=bt2020nc",
			"-frames:v", "12", "-vf", toneMaps[name], "-f", "null", "-")).Run()
		cancel()
		if err == nil {
			return name
		}
	}
	return ""
}

// trialEncode runs a second of 10-bit frames through the encoder: the input most likely to fail.
func trialEncode(ctx context.Context, ffmpeg, codec string, tuning ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	s := &Session{Plan: Plan{VideoCodec: codec}, encoder: codec, tuned: map[string][]string{codec: tuning}}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	args = append(args, s.deviceArgs()...)
	args = append(args,
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24,format=yuv420p10le",
		"-frames:v", "24",
	)
	args = append(args, s.videoArgs()...)
	args = append(args, "-f", "null", "-")

	out, err := proc.Hide(exec.CommandContext(ctx, ffmpeg, args...)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, lastLines(string(out), 3))
	}
	return nil
}
