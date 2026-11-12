package config

import "errors"

var (
	ErrPickCancelled = errors.New("no folder was picked")
	ErrPickBusy      = errors.New("a folder window is already open")
	ErrNoPicker      = errors.New("this computer has no folder window Yoru can open")
)
