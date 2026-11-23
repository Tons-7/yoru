package transcode

import (
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSheetFillsTheTilesAfterTheLastKeyframe(t *testing.T) {
	ffmpeg, _ := binaries(t)
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if b, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10:duration=61",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "250", "-sc_threshold", "0", clip).CombinedOutput(); err != nil {
		t.Fatalf("clip: %v: %s", err, b)
	}

	const duration, aspect = 61.0, 16.0 / 9.0
	NewThumbnails(ffmpeg, slog.New(slog.NewTextHandler(io.Discard, nil))).Generate(clip, dir, duration, aspect)
	var f *os.File
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if opened, err := os.Open(sheetPath(dir)); err == nil {
			f = opened
			break
		}
	}
	if f == nil {
		t.Fatal("no sheet was written")
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}

	plan, h := PlanSheet(duration), tileHeight(aspect)
	for i := range plan.Count {
		x0, y0 := (i%plan.Columns)*thumbWidth, (i/plan.Columns)*h
		var sum, n float64
		for y := y0; y < y0+h; y += 4 {
			for x := x0; x < x0+thumbWidth; x += 4 {
				r, g, b, _ := img.At(x, y).RGBA()
				sum += float64(r+g+b) / 3 / 257
				n++
			}
		}
		if mean := sum / n; mean < 16 {
			t.Errorf("tile %d of %d is black (mean %.0f)", i+1, plan.Count, mean)
		}
	}
}
