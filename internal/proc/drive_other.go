//go:build !windows

package proc

type Drive struct {
	Kind string
	Slow bool
}

func DriveOf(string) Drive { return Drive{Kind: "unknown"} }
