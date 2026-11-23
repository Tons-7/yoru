package utp

import "expvar"

var (
	ackSkippedResends      = expvar.NewInt("utpAckSkippedResends")
	deliveriesProcessed    = expvar.NewInt("utpDeliveriesProcessed")
	sentStatePackets       = expvar.NewInt("utpSentStatePackets")
	acksReceivedAheadOfSyn = expvar.NewInt("utpAcksReceivedAheadOfSyn")
	unexpectedPacketsRead  = expvar.NewInt("utpUnexpectedPacketsRead")
	unsentStatePackets     = expvar.NewInt("utpUnsentStatePackets")
	unusedReads            = expvar.NewInt("utpUnusedReads")
	unusedReadsDropped     = expvar.NewInt("utpUnusedReadsDropped")

	largestReceivedUTPPacket       int
	largestReceivedUTPPacketExpvar = expvar.NewInt("utpLargestReceivedPacket")
)
