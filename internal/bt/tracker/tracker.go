package tracker

import (
	"errors"
	"time"

	trHttp "yoru/internal/bt/tracker/http"
	"yoru/internal/bt/tracker/shared"
	"yoru/internal/bt/tracker/udp"
)

const (
	None      = shared.None
	Started   = shared.Started
	Stopped   = shared.Stopped
	Completed = shared.Completed
)

type AnnounceRequest = udp.AnnounceRequest

type AnnounceResponse = trHttp.AnnounceResponse

type Peer = trHttp.Peer

type AnnounceEvent = shared.AnnounceEvent

var ErrBadScheme = errors.New("unknown scheme")

const DefaultTrackerAnnounceTimeout = 15 * time.Second
