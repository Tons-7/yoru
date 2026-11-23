//go:build !windows

package tray

type Options struct {
	Tip    string
	Open   func()
	Exit   func()
	Ending func()
}

const Available = false

type Tray struct{}

func Start(Options) (*Tray, error) { return &Tray{}, nil }

func (*Tray) Close() {}

func Shown() bool { return false }
