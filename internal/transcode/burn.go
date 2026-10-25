package transcode

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// burnName is the subtitle file a burned session's passes read from their own folder, by a name that needs no
// escaping in a filter (a Windows path's drive colon would split the option).
func burnName(subtitles string) string {
	if ext := strings.ToLower(filepath.Ext(subtitles)); ext == ".srt" || ext == ".ssa" || ext == ".vtt" {
		return "burn" + ext
	}
	return "burn.ass"
}

// into copies the subtitles and their fonts into a session's folder; a picture track is the source's own.
func (b Burn) into(dir string) error {
	if b.Pictures > 0 {
		return nil
	}
	if err := copyInto(b.Subtitles, filepath.Join(dir, burnName(b.Subtitles))); err != nil {
		return fmt.Errorf("subtitles to draw in: %w", err)
	}
	if b.Fonts == "" {
		return nil
	}
	fonts := filepath.Join(dir, "fonts")
	if err := os.MkdirAll(fonts, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(b.Fonts)
	if err != nil {
		// None extracted: the system's fonts stand in, as a player without the file's would do.
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			copyInto(filepath.Join(b.Fonts, e.Name()), filepath.Join(fonts, e.Name()))
		}
	}
	return nil
}

func copyInto(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pictureOverlay lays a PGS track over the video once chain has run on it, delayed, scaled to the frame.
func pictureOverlay(track int, delay float64, chain string) string {
	picture := "[0:v:0]"
	if chain != "" {
		picture = "[pic]"
		chain = "[0:v:0]" + chain + "[pic];"
	}
	return fmt.Sprintf("%s[0:%d]setpts=PTS+%.3f/TB[sub];[sub]%sscale2ref[s][base];[base][s]overlay=eof_action=pass[v]",
		chain, track, delay, picture)
}

// burnFilter draws the session's subtitles in, from its folder (each pass runs there).
func (s *Session) burnFilter() string {
	name := "burn.ass"
	for _, n := range []string{"burn.ass", "burn.srt", "burn.ssa", "burn.vtt"} {
		if _, err := os.Stat(filepath.Join(s.dir, n)); err == nil {
			name = n
			break
		}
	}
	return "subtitles=" + name + ":fontsdir=fonts"
}
