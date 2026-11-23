package transcode

import (
	"fmt"
	"math"
	"runtime"
	"strings"

	"yoru/internal/sound"
)

func (s *Session) canDecodeInHardware() bool {
	v := s.Info.Video
	if v == nil {
		return false
	}
	if v.BitDepth > 8 && isH264(s.Plan.VideoCodec) {
		return false
	}
	return true
}

const (
	readRate     = 2.0
	keepReadRate = 4.0

	initialBurstSeconds = 60.0
)

func (s *Session) args(r *run) []string {
	offset := s.segmentStart(r.from)
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}

	if accel := hwaccel(s.encoder); accel != "" && !s.Plan.VideoCopy && s.canDecodeInHardware() && !s.burned &&
		len(s.pictureFilters()) == 0 {
		a = append(a, "-hwaccel", accel)
	}
	a = append(a, s.deviceArgs()...)

	switch {
	case offset <= 0:
	case s.sliced():
		a = append(a, "-seek_timestamp", "1", "-ss", fmt.Sprintf("%.3f", offset))
	default:
		seek := offset
		if s.Plan.VideoCopy {
			seek += 0.5
		}
		a = append(a, "-ss", fmt.Sprintf("%.3f", seek))
	}

	rate := readRate
	if s.want.Keep {
		rate = keepReadRate
		if s.Plan.VideoCopy || usesHardware(s.Plan.VideoCodec) {
			rate = 0
		}
	}
	if rate > 0 {
		a = append(a,
			"-readrate", fmt.Sprintf("%.1f", rate),
			"-readrate_initial_burst", fmt.Sprintf("%.0f", initialBurstSeconds),
		)
	}

	a = append(a, reconnectArgs(s.Source)...)
	video := []string{"-map", "0:v:0"}
	if s.burnPictures > 0 {
		graph := pictureOverlay(s.burnPictures, s.burnDelay, strings.Join(s.pictureFilters(), ","))
		if up := s.upload(); up != "" {
			graph = strings.TrimSuffix(graph, "[v]") + "[o];[o]" + up + "[v]"
		}
		video = []string{"-filter_complex", graph, "-map", "[v]"}
	}
	a = append(a, "-i", s.Source)
	a = append(a, video...)
	a = append(a,
		"-map", fmt.Sprintf("0:a:%d?", s.audioTrack),
		"-map_metadata", "-1",
		"-map_chapters", "-1",
	)

	a = append(a, s.videoArgs()...)
	a = append(a, s.audioArgs()...)

	cuts := []string{
		"-hls_time", fmt.Sprintf("%.0f", SegmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
		"-start_number", fmt.Sprint(r.from),
		"-hls_segment_filename", "%d.mp4",
	}
	if s.sliced() {
		cuts = []string{
			"-hls_time", "0.001",
			"-hls_list_size", "1",
			"-start_number", "0",
			"-hls_segment_filename", fmt.Sprintf("p%d_%%d.mp4", r.id),
		}
	}
	a = append(a,
		"-copyts",
		"-avoid_negative_ts", "disabled",
		"-max_muxing_queue_size", "1024",
		"-f", "hls",
		"-hls_segment_type", "fmp4",
	)
	a = append(a, cuts...)
	a = append(a,
		"-hls_segment_options", "movflags=+frag_discont+skip_sidx",
		"-hls_fmp4_init_filename", "init.mp4",
		"-y", "playlist.m3u8",
	)
	return a
}

func (s *Session) videoArgs() []string {
	if s.Plan.VideoCopy {
		if s.Info != nil && s.Info.Video != nil && s.Info.Video.Codec == "hevc" {
			return []string{"-c:v", "copy", "-tag:v", "hvc1"}
		}
		return []string{"-c:v", "copy"}
	}

	codec := s.Plan.VideoCodec
	args := []string{"-c:v", codec}
	filters := s.pictureFilters()
	if s.burned && s.burnPictures == 0 {
		filters = append(filters, s.burnFilter())
	}
	if up := s.upload(); up != "" {
		filters = append(filters, up)
	}
	if len(filters) > 0 && s.burnPictures == 0 {
		args = append([]string{"-vf", strings.Join(filters, ",")}, args...)
	}

	gop := int(SegmentSeconds * 24)
	if s.Info != nil && s.Info.Video != nil && s.Info.Video.FPS > 0 {
		gop = int(math.Round(SegmentSeconds * s.Info.Video.FPS))
	}
	if usesHardware(codec) {
		args = append(args, "-g", fmt.Sprint(gop), "-keyint_min", fmt.Sprint(gop))
	} else {
		args = append(args,
			"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%.0f)", SegmentSeconds),
			"-sc_threshold", "0",
			"-preset", "veryfast",
		)
	}

	switch {
	case strings.Contains(codec, "nvenc"):
		args = append(args, "-preset", "p4", "-rc", "vbr", "-cq", "23")
	case strings.Contains(codec, "qsv"):
		args = append(args, "-global_quality", "23")
	case strings.Contains(codec, "amf"):
		args = append(args, "-quality", "balanced", "-rc", "vbr_latency")
	case strings.Contains(codec, "videotoolbox"):
		args = append(args, "-b:v", "8M")
	case strings.Contains(codec, "vaapi"):
		args = append(args, "-qp", "23")
	case codec == "libx264":
		args = append(args, "-crf", "21", "-tune", "animation")
	}
	args = append(args, s.tuned[codec]...)
	if rate := s.Plan.MaxRate; rate > 0 {
		args = append(args, "-maxrate", fmt.Sprint(rate), "-bufsize", fmt.Sprint(2*rate))
	}

	if isH264(codec) && s.upload() == "" {
		args = append(args, "-pix_fmt", "yuv420p")
	}
	return args
}

func hwaccel(encoder string) string {
	switch {
	case !usesHardware(encoder):
		return ""
	case runtime.GOOS == "windows":
		return "d3d11va"
	case strings.Contains(encoder, "videotoolbox"):
		return "videotoolbox"
	case runtime.GOOS == "linux" && strings.Contains(encoder, "nvenc"):
		return "cuda"
	}
	return ""
}

func (s *Session) deviceArgs() []string {
	if strings.Contains(s.Plan.VideoCodec, "vaapi") && !s.Plan.VideoCopy {
		return []string{"-vaapi_device", vaapiDevice}
	}
	return nil
}

func (s *Session) upload() string {
	if strings.Contains(s.Plan.VideoCodec, "vaapi") && !s.Plan.VideoCopy {
		return "format=nv12,hwupload"
	}
	return ""
}

var toneMaps = map[string]string{
	"libplacebo": "libplacebo=tonemapping=bt.2390:colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv:format=yuv420p",
	"zscale":     "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p",
}

func (s *Session) pictureFilters() []string {
	var f []string
	if s.Plan.Deinterlace {
		f = append(f, "bwdif=mode=send_frame:deint=interlaced")
	}
	if tm := toneMaps[s.toneMapper]; s.Plan.ToneMap && tm != "" {
		f = append(f, tm)
	}
	if s.Plan.Height > 0 && s.Info != nil && s.Info.Video != nil && s.Info.Video.Height > s.Plan.Height {
		f = append(f, fmt.Sprintf("scale=-2:%d", s.Plan.Height))
	}
	if s.Plan.Soften {
		f = append(f, "format=rgb24,photosensitivity,format=yuv420p")
	}
	return f
}

func (s *Session) audioArgs() []string {
	if s.Plan.AudioCopy {
		return []string{"-c:a", "copy"}
	}
	if filter := s.soundFilter(); filter != "" {
		a := []string{"-af", filter, "-c:a", "aac", "-b:a", "192k"}
		if s.Plan.AudioCodec == "flac" {
			a = []string{"-af", filter + ",asetnsamples=n=4096:p=0", "-c:a", "flac"}
		}
		return a
	}
	in := 0
	if s.Info != nil && s.audioTrack < len(s.Info.Audio) {
		in = s.Info.Audio[s.audioTrack].Channels
	}
	out := s.Plan.Channels
	if out == 0 && s.Plan.AudioCodec == "flac" {
		out = min(in, 6)
	}
	var a []string
	switch {
	case out == 2 && in > 2:
		a = []string{"-af", sound.Stereo}
	case out == 6 && s.Plan.AudioCodec == "aac":
		a = []string{"-af", "aformat=channel_layouts=5.1"}
	case out > 0 && out != in:
		a = []string{"-ac", fmt.Sprint(out)}
	}
	if s.Plan.AudioCodec == "flac" {
		return append(a, "-c:a", "flac")
	}
	rate := "192k"
	switch {
	case s.Plan.MaxRate > 0:
		rate = "128k"
	case out > 2:
		rate = "384k"
	}
	return append(a, "-c:a", "aac", "-b:a", rate)
}

func reconnectArgs(source string) []string {
	if !remote(source) {
		return nil
	}
	return []string{"-reconnect", "1", "-reconnect_delay_max", "10"}
}

func remote(source string) bool {
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func usesHardware(codec string) bool {
	for _, suffix := range []string{"nvenc", "qsv", "amf", "vaapi", "videotoolbox"} {
		if strings.Contains(codec, suffix) {
			return true
		}
	}
	return false
}

func isH264(codec string) bool {
	return strings.HasPrefix(codec, "libx264") || strings.HasPrefix(codec, "h264")
}
