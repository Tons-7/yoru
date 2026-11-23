package peer_protocol

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/bits"
	"strings"

	"github.com/anacrolix/missinggo/v2/panicif"

	"yoru/internal/bt/internal/ctxrw"
	"yoru/internal/bt/metainfo"
)

type ExtensionBit uint

const (
	ExtensionBitDht                          = 0
	ExtensionBitFast                         = 2
	ExtensionBitV2Upgrade                    = 4
	ExtensionBitAzureusExtensionNegotiation1 = 16
	ExtensionBitAzureusExtensionNegotiation2 = 17
	ExtensionBitLtep                         = 20
	ExtensionBitLocationAwareProtocol        = 43
	ExtensionBitAzureusMessagingProtocol     = 63
)

func handshakeWriter(w io.Writer, bb <-chan []byte, done chan<- error) {
	var err error
	for b := range bb {
		_, err = w.Write(b)
		if err != nil {
			break
		}
	}
	done <- err
}

type (
	PeerExtensionBits [8]byte
)

var bitTags = []struct {
	bit ExtensionBit
	tag string
}{
	{ExtensionBitAzureusMessagingProtocol, "amp"},
	{ExtensionBitLocationAwareProtocol, "loc"},
	{ExtensionBitLtep, "ltep"},
	{ExtensionBitAzureusExtensionNegotiation2, "azen2"},
	{ExtensionBitAzureusExtensionNegotiation1, "azen1"},
	{ExtensionBitV2Upgrade, "v2"},
	{ExtensionBitFast, "fast"},
	{ExtensionBitDht, "dht"},
}

func (pex PeerExtensionBits) String() string {
	pexHex := hex.EncodeToString(pex[:])
	tags := make([]string, 0, len(bitTags)+1)
	for _, bitTag := range bitTags {
		if pex.GetBit(bitTag.bit) {
			tags = append(tags, bitTag.tag)
			pex.SetBit(bitTag.bit, false)
		}
	}
	unknownCount := bits.OnesCount64(binary.LittleEndian.Uint64(pex[:]))
	if unknownCount != 0 {
		tags = append(tags, fmt.Sprintf("%v unknown", unknownCount))
	}
	return fmt.Sprintf("%v (%s)", pexHex, strings.Join(tags, ", "))

}

func NewPeerExtensionBytes(bits ...ExtensionBit) (ret PeerExtensionBits) {
	for _, b := range bits {
		ret.SetBit(b, true)
	}
	return
}

func (pex PeerExtensionBits) SupportsExtended() bool {
	return pex.GetBit(ExtensionBitLtep)
}

func (pex PeerExtensionBits) SupportsDHT() bool {
	return pex.GetBit(ExtensionBitDht)
}

func (pex PeerExtensionBits) SupportsFast() bool {
	return pex.GetBit(ExtensionBitFast)
}

func (pex *PeerExtensionBits) SetBit(bit ExtensionBit, on bool) {
	if on {
		pex[7-bit/8] |= 1 << (bit % 8)
	} else {
		pex[7-bit/8] &^= 1 << (bit % 8)
	}
}

func (pex PeerExtensionBits) GetBit(bit ExtensionBit) bool {
	return pex[7-bit/8]&(1<<(bit%8)) != 0
}

type HandshakeResult struct {
	PeerExtensionBits
	PeerID [20]byte
	metainfo.Hash
}

func Handshake(
	ctx context.Context,
	sock io.ReadWriter,
	ih *metainfo.Hash,
	peerID [20]byte,
	extensions PeerExtensionBits,
) (
	res HandshakeResult, err error,
) {
	defer ctxrw.Bind(ctx, sock)()
	postCh := make(chan []byte, 4)
	writeDone := make(chan error, 1)
	go handshakeWriter(sock, postCh, writeDone)

	defer func() {
		close(postCh)
		if err != nil {
			return
		}
		err = <-writeDone
		if err != nil {
			err = fmt.Errorf("error writing: %w", err)
		}
	}()

	post := func(bb []byte) {
		panicif.SendBlocks(postCh, bb)
	}

	post(protocolBytes())
	post(extensions[:])
	if ih != nil {
		post(ih[:])
		post(peerID[:])
	}

	b := make([]byte, 68)
	_, err = io.ReadFull(sock, b[:])
	if err != nil {
		return res, fmt.Errorf("while reading: %w", err)
	}

	p := b[:len(Protocol)]
	if string(p) != Protocol {
		return res, fmt.Errorf("unexpected protocol string %q", string(p))
	}
	b = b[len(p):]
	read := func(dst []byte) {
		n := copy(dst, b)
		panicif.NotEq(n, len(dst))
		b = b[n:]
	}
	read(res.PeerExtensionBits[:])
	read(res.Hash[:])
	read(res.PeerID[:])
	panicif.NotEq(len(b), 0)

	if ih == nil {
		post(res.Hash[:])
		post(peerID[:])
	}

	return
}
