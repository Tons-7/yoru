package bt

import (
	"yoru/internal/bt/dialer"
)

type (
	Dialer        = dialer.T
	NetworkDialer = dialer.WithNetwork
)

var DefaultNetDialer = &dialer.Default
