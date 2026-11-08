package transcode

import "testing"

func TestFrameRateReadsWhatFfprobeReports(t *testing.T) {
	cases := []struct {
		avg, base string
		want      float64
	}{
		{"24000/1001", "24000/1001", 23.976},
		{"24/1", "24/1", 24},
		{"30000/1001", "30000/1001", 29.97},
		{"0/0", "24000/1001", 23.976},
		{"", "25/1", 25},
		{"0/0", "0/0", 0},
		{"90000/1", "90000/1", 0},
	}
	for _, c := range cases {
		if got := frameRate(c.avg, c.base); got != c.want {
			t.Errorf("frameRate(%q, %q) = %v, want %v", c.avg, c.base, got, c.want)
		}
	}
}
