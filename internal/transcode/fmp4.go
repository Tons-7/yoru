package transcode

import (
	"encoding/binary"
	"errors"
)

// The few ISO-BMFF boxes needed to say when a fragment's picture starts: a sliced pass names its files by
// count, not time, so each is placed by what is inside it.

// fmp4Track is what the init segment says about one track.
type fmp4Track struct {
	timescale uint32
	video     bool
	// shift is the edit list's offset, in the track's units: media time minus empty edits ahead of it.
	shift int64
}

var errNoBox = errors.New("box not found")

// box finds the first child of type typ in buf, a run of boxes.
func box(buf []byte, typ string) ([]byte, error) {
	if found := boxes(buf, typ, 1); len(found) > 0 {
		return found[0], nil
	}
	return nil, errNoBox
}

// boxes returns the children of type typ in buf, at most limit of them (0 for all).
func boxes(buf []byte, typ string, limit int) [][]byte {
	var out [][]byte
	for len(buf) >= 8 && (limit == 0 || len(out) < limit) {
		size := uint64(binary.BigEndian.Uint32(buf))
		head := uint64(8)
		switch size {
		case 1:
			if len(buf) < 16 {
				return out
			}
			size, head = binary.BigEndian.Uint64(buf[8:]), 16
		case 0:
			size = uint64(len(buf))
		}
		if size < head || size > uint64(len(buf)) {
			return out
		}
		if string(buf[4:8]) == typ {
			out = append(out, buf[head:size])
		}
		buf = buf[size:]
	}
	return out
}

// nested walks into boxes, first match at each level.
func nested(buf []byte, types ...string) ([]byte, error) {
	var err error
	for _, t := range types {
		if buf, err = box(buf, t); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// initTracks reads an init segment's tracks by ID.
func initTracks(init []byte) (map[uint32]fmp4Track, error) {
	moov, err := box(init, "moov")
	if err != nil {
		return nil, err
	}
	mvhd, err := box(moov, "mvhd")
	if err != nil || len(mvhd) < 20 {
		return nil, errNoBox
	}
	movieScale := binary.BigEndian.Uint32(mvhd[12:])
	if mvhd[0] == 1 {
		if len(mvhd) < 24 {
			return nil, errNoBox
		}
		movieScale = binary.BigEndian.Uint32(mvhd[20:])
	}

	tracks := map[uint32]fmp4Track{}
	for _, trak := range boxes(moov, "trak", 0) {
		tkhd, err := box(trak, "tkhd")
		if err != nil || len(tkhd) < 24 {
			continue
		}
		id := binary.BigEndian.Uint32(tkhd[12:])
		if tkhd[0] == 1 {
			if len(tkhd) < 24 {
				continue
			}
			id = binary.BigEndian.Uint32(tkhd[20:])
		}
		mdhd, err := nested(trak, "mdia", "mdhd")
		if err != nil || len(mdhd) < 24 {
			continue
		}
		scale := binary.BigEndian.Uint32(mdhd[12:])
		if mdhd[0] == 1 {
			if len(mdhd) < 32 {
				continue
			}
			scale = binary.BigEndian.Uint32(mdhd[20:])
		}
		hdlr, err := nested(trak, "mdia", "hdlr")
		if err != nil || len(hdlr) < 12 || scale == 0 {
			continue
		}
		t := fmp4Track{timescale: scale, video: string(hdlr[8:12]) == "vide"}
		if elst, err := nested(trak, "edts", "elst"); err == nil {
			t.shift = editShift(elst, scale, movieScale)
		}
		tracks[id] = t
	}
	return tracks, nil
}

// editShift is what an edit list takes off a sample's time: the first real edit's media time, less any empty
// edits before it (which delay the track, in movie units).
func editShift(elst []byte, scale, movieScale uint32) int64 {
	if len(elst) < 8 {
		return 0
	}
	v1 := elst[0] == 1
	n := binary.BigEndian.Uint32(elst[4:])
	at := 8
	var delay int64
	for range n {
		var dur uint64
		var media int64
		if v1 {
			if at+20 > len(elst) {
				return 0
			}
			dur = binary.BigEndian.Uint64(elst[at:])
			media = int64(binary.BigEndian.Uint64(elst[at+8:]))
			at += 20
		} else {
			if at+12 > len(elst) {
				return 0
			}
			dur = uint64(binary.BigEndian.Uint32(elst[at:]))
			media = int64(int32(binary.BigEndian.Uint32(elst[at+4:])))
			at += 12
		}
		if media == -1 {
			if movieScale > 0 {
				delay += int64(dur) * int64(scale) / int64(movieScale)
			}
			continue
		}
		return media - delay
	}
	return -delay
}

// pictureStart is when a fragment's first video sample shows, in seconds; false when it holds no picture.
func pictureStart(tracks map[uint32]fmp4Track, seg []byte) (float64, bool) {
	moof, err := box(seg, "moof")
	if err != nil {
		return 0, false
	}
	for _, traf := range boxes(moof, "traf", 0) {
		tfhd, err := box(traf, "tfhd")
		if err != nil || len(tfhd) < 8 {
			continue
		}
		t, ok := tracks[binary.BigEndian.Uint32(tfhd[4:])]
		if !ok || !t.video {
			continue
		}
		tfdt, err := box(traf, "tfdt")
		if err != nil || len(tfdt) < 8 {
			continue
		}
		decode := int64(binary.BigEndian.Uint32(tfdt[4:]))
		if tfdt[0] == 1 {
			if len(tfdt) < 12 {
				continue
			}
			decode = int64(binary.BigEndian.Uint64(tfdt[4:]))
		}
		cto, ok := firstOffset(traf)
		if !ok {
			continue
		}
		return float64(decode+cto-t.shift) / float64(t.timescale), true
	}
	return 0, false
}

// firstOffset is the first sample's composition offset in a traf's first trun (0 when the run carries none).
func firstOffset(traf []byte) (int64, bool) {
	trun, err := box(traf, "trun")
	if err != nil || len(trun) < 8 {
		return 0, false
	}
	flags := binary.BigEndian.Uint32(trun[0:]) & 0xffffff
	if binary.BigEndian.Uint32(trun[4:]) == 0 {
		return 0, false
	}
	at := 8
	if flags&0x1 != 0 { // data offset
		at += 4
	}
	if flags&0x4 != 0 { // first sample flags
		at += 4
	}
	if flags&0x100 != 0 { // sample duration
		at += 4
	}
	if flags&0x200 != 0 { // sample size
		at += 4
	}
	if flags&0x400 != 0 { // sample flags
		at += 4
	}
	if flags&0x800 == 0 {
		return 0, true
	}
	if at+4 > len(trun) {
		return 0, false
	}
	raw := binary.BigEndian.Uint32(trun[at:])
	if trun[0] == 1 {
		return int64(int32(raw)), true
	}
	return int64(raw), true
}
