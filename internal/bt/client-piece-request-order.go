package bt

import (
	requestStrategy "yoru/internal/bt/internal/request-strategy"
	"yoru/internal/bt/storage"
)

type clientPieceRequestOrderKeyTypes interface {
	storage.TorrentCapacity | *Torrent
}

type clientPieceRequestOrderKey[T clientPieceRequestOrderKeyTypes] struct {
	inner T
}

type clientPieceRequestOrderRegularTorrentKey clientPieceRequestOrderKey[*Torrent]

func (c clientPieceRequestOrderRegularTorrentKey) getRequestStrategyInput(cl *Client) requestStrategy.Input {
	return requestStrategyInputSingleTorrent{
		requestStrategyInputCommon: cl.getRequestStrategyInputCommon(),
		t:                          c.inner,
	}
}

type clientPieceRequestOrderSharedStorageTorrentKey clientPieceRequestOrderKey[storage.TorrentCapacity]

func (c clientPieceRequestOrderSharedStorageTorrentKey) getRequestStrategyInput(cl *Client) requestStrategy.Input {
	return requestStrategyInputMultiTorrent{
		requestStrategyInputCommon: cl.getRequestStrategyInputCommon(),
		torrents:                   cl.torrentsByShortHash,
		capFunc:                    c.inner,
	}
}

type clientPieceRequestOrderKeySumType interface {
	getRequestStrategyInput(cl *Client) requestStrategy.Input
}

type clientPieceRequestOrderValue struct {
	torrents map[*Torrent]struct{}
	pieces   *requestStrategy.PieceRequestOrder
}
