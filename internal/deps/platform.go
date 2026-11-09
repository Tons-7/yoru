package deps

import (
	"fmt"
	"runtime"
)

func exeExtFor(goos string) string {
	if goos == "windows" {
		return ".exe"
	}
	return ""
}

func componentFiles(name string) []string { return componentFilesFor(name, runtime.GOOS) }

func componentFilesFor(name, goos string) []string {
	ext := exeExtFor(goos)
	switch name {
	case "ffmpeg":
		return []string{"ffmpeg" + ext, "ffprobe" + ext}
	case "mpv":
		if goos == "windows" {
			return []string{"mpv.exe", "mpv.com"}
		}
		return []string{"mpv"}
	case "cloudflared":
		return []string{"cloudflared" + ext}
	}
	return nil
}

func ffmpegLinuxURL() (string, error) { return ffmpegLinuxURLFor(runtime.GOARCH) }

func ffmpegLinuxURLFor(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz", nil
	case "arm64":
		return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz", nil
	case "386":
		return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-i686-static.tar.xz", nil
	case "arm":
		return "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-armhf-static.tar.xz", nil
	}
	return "", fmt.Errorf("no static ffmpeg build for linux/%s", goarch)
}

func ManualCommand(name string) string {
	if runtime.GOOS == "windows" || name == "anime4k" || name == "flyhigh" || name == "cloudflared" {
		return ""
	}
	if name == "ffmpeg" && runtime.GOOS == "linux" {
		if _, err := ffmpegLinuxURL(); err == nil {
			return ""
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return "brew install " + name
	case "linux":
		return "sudo apt install " + name
	}
	return ""
}

func manualInstall(name string) error {
	how := ManualCommand(name)
	if how == "" {
		how = "your package manager"
	}
	return fmt.Errorf("Yoru cannot download %s on %s; install it with `%s` (Yoru finds it on PATH)",
		name, runtime.GOOS, how)
}
