package transcode

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The PGS overlay runs in ffmpeg as written, over a picture smaller than the track's canvas, delayed.
func TestPictureOverlayRunsInFFmpeg(t *testing.T) {
	ffmpeg, _ := binaries(t)
	dir := t.TempDir()
	sup := filepath.Join(dir, "box.sup")
	if err := os.WriteFile(sup, boxSup(), 0o644); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(dir, "clip.mkv")
	if b, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=10:duration=4", "-i", sup,
		"-map", "0:v", "-map", "1:s", "-c:v", "libx264", "-preset", "ultrafast", "-c:s", "copy", clip).CombinedOutput(); err != nil {
		t.Fatalf("clip: %v: %s", err, b)
	}
	// Alone, and after the picture's own changes (here a cap and softened flashing).
	for _, chain := range []string{"", "scale=-2:240,format=rgb24,photosensitivity,format=yuv420p"} {
		filter := pictureOverlay(1, 0.5, chain)
		if !strings.Contains(filter, "setpts=PTS+0.500/TB") || !strings.Contains(filter, "scale2ref") {
			t.Errorf("filter %q, want the delay and the scale to the picture", filter)
		}
		out := filepath.Join(dir, "frame.png")
		if b, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", clip,
			"-filter_complex", filter, "-map", "[v]", "-ss", "3", "-frames:v", "1", out).CombinedOutput(); err != nil {
			t.Fatalf("overlay after %q: %v: %s", chain, err, b)
		}
	}
}

// boxSup is a PGS file of one white box shown from 1 s, then cleared at 3.9 s.
func boxSup() []byte {
	seg := func(typ byte, seconds float64, data []byte) []byte {
		pts := uint32(seconds * 90000)
		head := []byte{'P', 'G', byte(pts >> 24), byte(pts >> 16), byte(pts >> 8), byte(pts), 0, 0, 0, 0, typ,
			byte(len(data) >> 8), byte(len(data))}
		return append(head, data...)
	}
	u16 := func(n int) []byte { return []byte{byte(n >> 8), byte(n)} }
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	const w, h, x, y = 300, 60, 490, 600
	display := func(seconds float64, number int, show bool) []byte {
		pcs := cat(u16(1280), u16(720), []byte{0x10}, u16(number))
		if show {
			pcs = cat(pcs, []byte{0x80, 0, 0, 1}, u16(0), []byte{0, 0}, u16(x), u16(y))
		} else {
			pcs = cat(pcs, []byte{0, 0, 0, 0})
		}
		out := cat(seg(0x16, seconds, pcs), seg(0x17, seconds, cat([]byte{1, 0}, u16(x), u16(y), u16(w), u16(h))))
		if show {
			out = cat(out, seg(0x14, seconds, []byte{0, 0, 1, 235, 128, 128, 255}))
			var rle []byte
			for range h {
				rle = append(rle, 0, 0xc0|byte(w>>8), byte(w&0xff), 1, 0, 0)
			}
			size := len(rle) + 4
			out = cat(out, seg(0x15, seconds, cat([]byte{0, 0, 0, 0xc0, byte(size >> 16), byte(size >> 8), byte(size)}, u16(w), u16(h), rle)))
		}
		return cat(out, seg(0x80, seconds, nil))
	}
	return cat(display(1, 0, true), display(3.9, 1, false))
}
