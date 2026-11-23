package sound

import (
	"fmt"
	"math"
	"strings"
)

type Options struct {
	Voice      string `json:"voice"`
	Night      bool   `json:"night"`
	Headphones bool   `json:"headphones"`
	Mono       bool   `json:"mono"`
	Boost      string `json:"boost"`
}

var boostLevels = map[string]float64{"medium": 6, "high": 12}

var voiceLevels = map[string]float64{"medium": 6, "high": 12}

var Levels = []string{"off", "medium", "high"}

func ValidVoice(level string) bool {
	_, ok := voiceLevels[level]
	return ok || level == "" || level == "off"
}

func ValidBoost(level string) bool {
	_, ok := boostLevels[level]
	return ok || level == "" || level == "off"
}

func Normalized(o Options) Options {
	if o.Voice == "off" {
		o.Voice = ""
	}
	if o.Boost == "off" {
		o.Boost = ""
	}
	return o
}

func (o Options) Any() bool {
	o = Normalized(o)
	return o.Voice != "" || o.Night || o.Headphones || o.Mono || o.Boost != ""
}

func (o Options) Level() string {
	if o = Normalized(o); o.Voice == "" {
		return "off"
	}
	return o.Voice
}

const (
	presence   = "equalizer=f=2500:t=q:w=1:g=2"
	night      = "acompressor=threshold=0.0631:ratio=3:attack=20:release=250:makeup=2"
	headphones = "crossfeed=strength=0.3:range=0.5"
	mono       = "pan=stereo|FL=0.5*FL+0.5*FR|FR=0.5*FL+0.5*FR"
	limiter    = "alimiter=limit=0.98:level=0:latency=1"
)

const front, rear = 0.369, 0.262

var Stereo = "aformat=channel_layouts=5.1," + fold(front)

func fold(centre float64) string {
	return fmt.Sprintf("pan=stereo|FL=%.3f*FL+%.3f*FC+%.3f*BL|FR=%.3f*FR+%.3f*FC+%.3f*BR", front, centre, rear, front, centre, rear)
}

func Chain(o Options, channels int) string {
	o = Normalized(o)
	if !o.Any() {
		return ""
	}
	if channels <= 0 {
		channels = 2
	}
	var parts []string
	switch {
	case channels == 1:
		if o.Voice != "" {
			parts = append(parts, presence)
		}
		parts = append(parts, "aformat=channel_layouts=stereo")
	case channels == 2:
		if gain, ok := voiceLevels[o.Voice]; ok {
			lift := math.Pow(10, gain/20)
			enhance, pan := 2.0, (lift-1)/2
			if gain >= 12 {
				enhance, pan = 3, (lift-1)/3
			}
			parts = append(parts,
				fmt.Sprintf("dialoguenhance=original=0:enhance=%g:voice=2", enhance),
				presence+":c=FC",
				fmt.Sprintf("pan=stereo|FL=FL+%.3f*FC|FR=FR+%.3f*FC", pan, pan))
		}
	default:
		if gain, ok := voiceLevels[o.Voice]; ok {
			parts = append(parts, "aformat=channel_layouts=5.1", presence+":c=FC", fold(front*math.Pow(10, gain/20)))
		} else {
			parts = append(parts, Stereo)
		}
	}
	if o.Night {
		parts = append(parts, night)
	}
	if o.Headphones && !o.Mono {
		parts = append(parts, headphones)
	}
	if o.Mono {
		parts = append(parts, mono)
	}
	if gain, ok := boostLevels[o.Boost]; ok {
		parts = append(parts, fmt.Sprintf("volume=%gdB", gain))
	}
	return strings.Join(append(parts, limiter), ",")
}

func MPVArgs(o Options) []string {
	chain := Chain(o, 2)
	if chain == "" {
		return nil
	}
	return []string{"--audio-channels=stereo", "--af=lavfi=[aformat=channel_layouts=stereo," + chain + "]"}
}

func VLCArgs(o Options) []string {
	o = Normalized(o)
	if !o.Any() {
		return nil
	}
	var filters, args []string
	if o.Voice != "" {
		bands := "0 0 0 0 3 4 2 0 0 0"
		if o.Voice == "high" {
			bands = "0 0 0 0 5 7 3 0 0 0"
		}
		filters = append(filters, "equalizer")
		args = append(args, "--equalizer-preamp=0", "--equalizer-bands="+bands)
	}
	if o.Night {
		filters = append(filters, "compressor")
		args = append(args, "--compressor-threshold=-24", "--compressor-ratio=3", "--compressor-attack=20",
			"--compressor-release=250", "--compressor-makeup-gain=6")
	}
	switch {
	case o.Mono:
		args = append(args, "--stereo-mode=7")
	case o.Headphones:
		args = append(args, "--stereo-mode=6")
	}
	if gain, ok := boostLevels[o.Boost]; ok {
		args = append(args, fmt.Sprintf("--gain=%g", math.Pow(10, gain/20)))
	}
	if len(filters) > 0 {
		args = append([]string{"--audio-filter=" + strings.Join(filters, ":")}, args...)
	}
	return args
}
