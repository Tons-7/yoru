package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

const infoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Yoru</string>
	<key>CFBundleDisplayName</key><string>Yoru</string>
	<key>CFBundleIdentifier</key><string>app.yoru.Yoru</string>
	<key>CFBundleExecutable</key><string>yoru</string>
	<key>CFBundleIconFile</key><string>yoru</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>LSMinimumSystemVersion</key><string>12.0</string>
	<key>LSUIElement</key><true/>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
`

type entry struct {
	name string
	body []byte
	mode fs.FileMode
}

func main() {
	out := flag.String("out", "", "the zip to write")
	app := flag.Bool("app", false, "pack as Yoru.app: the program named yoru, an Info.plist and the -icon")
	icon := flag.String("icon", "", "with -app: a 512px PNG for the app's icon")
	exec := flag.String("exec", "", "comma-separated entry names that are programs (0755)")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "unixzip: -out and at least one name=path are needed")
		os.Exit(2)
	}
	programs := map[string]bool{}
	for name := range strings.SplitSeq(*exec, ",") {
		programs[name] = name != ""
	}

	var entries []entry
	for _, arg := range flag.Args() {
		name, path, ok := strings.Cut(arg, "=")
		if !ok {
			fail(fmt.Errorf("%q is not name=path", arg))
		}
		body, err := os.ReadFile(path)
		if err != nil {
			fail(err)
		}
		mode := fs.FileMode(0o644)
		if programs[name] || (*app && name == "yoru") {
			mode = 0o755
		}
		switch {
		case *app && name == "yoru":
			name = "Yoru.app/Contents/MacOS/yoru"
		case *app:
			name = "Yoru.app/Contents/Resources/" + name
		}
		entries = append(entries, entry{name, body, mode})
	}
	if *app {
		png, err := os.ReadFile(*icon)
		if err != nil {
			fail(fmt.Errorf("-icon: %w", err))
		}
		entries = append(entries,
			entry{"Yoru.app/Contents/Info.plist", []byte(infoPlist), 0o644},
			entry{"Yoru.app/Contents/Resources/yoru.icns", icns(png), 0o644})
	}
	if err := write(*out, entries); err != nil {
		fail(err)
	}
}

func icns(png []byte) []byte {
	var b bytes.Buffer
	b.WriteString("icns")
	binary.Write(&b, binary.BigEndian, uint32(8+8+len(png)))
	b.WriteString("ic09")
	binary.Write(&b, binary.BigEndian, uint32(8+len(png)))
	b.Write(png)
	return b.Bytes()
}

func write(path string, entries []entry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: time.Now()}
		h.SetMode(e.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := w.Write(e.body); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "unixzip:", err)
	os.Exit(1)
}
