//go:build !windows

// Package tray is Yoru's notification-area icon, which exists on Windows alone.
package tray

// Options says what the icon does; unused off Windows.
type Options struct {
	Tip    string
	Open   func()
	Exit   func()
	Ending func()
}

// Available: this system shows Yoru's icon.
const Available = false

// Tray is a no-op off Windows.
type Tray struct{}

func Start(Options) (*Tray, error) { return &Tray{}, nil }

func (*Tray) Close() {}
