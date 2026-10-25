package transcode

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Half downloaded, holes zeroed: tiles come from the part on disk, as the whole sheet's, and none from holes.
func TestPartialSheetFromThePartsOnDisk(t *testing.T) {
	ffmpeg, _ := binaries(t)
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if b, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=120",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "48", "-sc_threshold", "0", clip).CombinedOutput(); err != nil {
		t.Fatalf("clip: %v: %s", err, b)
	}
	whole, err := os.ReadFile(clip)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(len(whole))
	have := []Span{{0, 64 << 10}, {size * 40 / 100, size * 70 / 100}}
	holey := make([]byte, size)
	for _, s := range have {
		copy(holey[s.From:s.To], whole[s.From:s.To])
	}
	downloading := filepath.Join(dir, "downloading.mkv")
	if err := os.WriteFile(downloading, holey, 0o644); err != nil {
		t.Fatal(err)
	}

	const duration, aspect = 120.0, 16.0 / 9.0
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	partDir, wholeDir := filepath.Join(dir, "partial"), filepath.Join(dir, "whole")
	th := NewThumbnails(ffmpeg, quiet)
	plan, h := PlanSheet(duration), tileHeight(aspect)
	made, err := th.fill(context.Background(), &partial{failed: map[int]time.Time{}}, downloading, partDir, plan, h, have)
	if err != nil {
		t.Fatal(err)
	}
	if err := layOut(partDir, plan, h); err != nil {
		t.Fatal(err)
	}
	sheet, ready := th.Read(partDir, duration, aspect)
	if ready || !sheet.Ready || !sheet.Partial || sheet.Version != made {
		t.Fatalf("read = %+v, %v; want a partial sheet of the %d tiles made", sheet, ready, made)
	}
	// The bytes on disk hold 40% to 70% of a steady clip: tiles there, none well outside.
	for i := range plan.Count {
		at := float64(i) * plan.Interval / duration
		if sheet.Have[i] == '1' && (at < 0.35 || at > 0.75) {
			t.Errorf("tile %d at %.0f%% made from a hole", i, at*100)
		}
		if sheet.Have[i] == '0' && at > 0.45 && at < 0.65 {
			t.Errorf("tile %d at %.0f%% missing though on disk", i, at*100)
		}
	}

	th.Generate(clip, wholeDir, duration, aspect)
	if err := os.MkdirAll(wholeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var full image.Image
	for deadline := time.Now().Add(time.Minute); full == nil && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		full = decodeJPEG(sheetPath(wholeDir))
	}
	part := decodeJPEG(partialSheet(partDir))
	if full == nil || part == nil {
		t.Fatal("a sheet is missing")
	}
	for i := range plan.Count {
		if sheet.Have[i] != '1' {
			continue
		}
		x0, y0 := (i%plan.Columns)*thumbWidth, (i/plan.Columns)*h
		var diff, n float64
		for y := y0; y < y0+h; y += 4 {
			for x := x0; x < x0+thumbWidth; x += 4 {
				a, _, _, _ := full.At(x, y).RGBA()
				b, _, _, _ := part.At(x, y).RGBA()
				diff += math.Abs(float64(a)-float64(b)) / 257
				n++
			}
		}
		if diff/n > 12 {
			t.Errorf("tile %d differs from the whole file's (mean %.0f)", i, diff/n)
		}
	}
}

func decodeJPEG(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

// The file's last cluster ends at its end: nothing can follow it, so its tiles are made.
func TestLeadingUpTakesTheLastCluster(t *testing.T) {
	clusters := []cluster{{at: 100, end: 200, time: 10}, {at: 200, end: 300, time: 15}}
	all := []Span{{0, 300}}
	if from, to, ok := leadingUp(clusters, all, all, 17, 300); !ok || from != 100 || to != 300 {
		t.Errorf("leadingUp at the end = %d, %d, %v; want 100, 300, true", from, to, ok)
	}
	// Mid-file with nothing on disk after it, a later cluster might start by want.
	if _, _, ok := leadingUp(clusters, all, all, 17, 1000); ok {
		t.Error("leadingUp with the bytes after the last cluster unknown went ahead")
	}
	// On disk after it but not yet scanned: a cluster there is unknown, so it doesn't go ahead either.
	if _, _, ok := leadingUp(clusters, []Span{{0, 400}}, all, 17, 1000); ok {
		t.Error("leadingUp went ahead over bytes on disk that no scan has read")
	}
}

// A cluster header split by a span's edge is found once the rest arrives.
func TestScanFindsAHeaderAcrossTheEdge(t *testing.T) {
	// ID, an 8-byte size, a CRC-32, Timestamp 1000 ticks, then a SimpleBlock: starting 20 before the span.
	head := []byte{0x1F, 0x43, 0xB6, 0x75, 0x01, 0, 0, 0, 0, 0, 0x10, 0, 0xBF, 0x84, 1, 2, 3, 4, 0xE7, 0x82, 0x03, 0xE8, 0xA3}
	data := make([]byte, 8192)
	copy(data[1000:], head)
	got := scanClusters(context.Background(), bytes.NewReader(data), Span{1020, 8192}, 1_000_000)
	if len(got) != 1 || got[0].at != 1000 || got[0].time != 1 {
		t.Errorf("clusters = %+v, want one at 1000, 1 s", got)
	}
	// The same bytes with frame data after the Timestamp are not a cluster.
	data[1000+len(head)-1] = 0x42
	if got := scanClusters(context.Background(), bytes.NewReader(data), Span{1020, 8192}, 1_000_000); len(got) != 0 {
		t.Errorf("a look-alike in frame data = %+v, want none", got)
	}
}

func TestUnscannedAndMerge(t *testing.T) {
	done := mergeSpans([]Span{{50, 60}, {0, 10}, {8, 20}})
	if len(done) != 2 || done[0] != (Span{0, 20}) || done[1] != (Span{50, 60}) {
		t.Fatalf("merged = %v", done)
	}
	got := unscanned(Span{5, 70}, done)
	if len(got) != 2 || got[0] != (Span{20, 50}) || got[1] != (Span{60, 70}) {
		t.Errorf("unscanned = %v, want [20,50) and [60,70)", got)
	}
}
