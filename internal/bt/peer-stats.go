package bt

type PeerStats struct {
	ConnStats

	DownloadRate        float64
	LastWriteUploadRate float64
	RemotePieceCount    int
}
