package transcode

import (
	"errors"
	"io"
	"os"
	"time"
)

// SegmentReader reads one segment as the player gets it: a single file, or a sliced segment's parts end to end.
type SegmentReader struct {
	files    []*os.File
	sizes    []int64
	size     int64
	off      int64
	Modified time.Time
}

var errSeek = errors.New("seek out of range")

// openParts opens files to read as one, shareably, so a replaced segment's parts can go while still being sent.
func openParts(paths []string, modified time.Time) (*SegmentReader, error) {
	r := &SegmentReader{Modified: modified}
	for _, p := range paths {
		f, err := openShared(p)
		if err != nil {
			r.Close()
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			r.Close()
			return nil, err
		}
		r.files = append(r.files, f)
		r.sizes = append(r.sizes, info.Size())
		r.size += info.Size()
		if modified.IsZero() || info.ModTime().After(r.Modified) {
			r.Modified = info.ModTime()
		}
	}
	return r, nil
}

func (r *SegmentReader) Size() int64 { return r.size }

func (r *SegmentReader) Read(p []byte) (int, error) {
	if r.off >= r.size {
		return 0, io.EOF
	}
	inner := r.off
	for i, size := range r.sizes {
		if inner >= size {
			inner -= size
			continue
		}
		n, err := r.files[i].ReadAt(p[:min(int64(len(p)), size-inner)], inner)
		r.off += int64(n)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if n == 0 && err == nil {
			// The part is shorter than it was when opened: it never is, short of the disk going.
			return 0, io.ErrUnexpectedEOF
		}
		return n, err
	}
	return 0, io.EOF
}

func (r *SegmentReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.off
	case io.SeekEnd:
		offset += r.size
	default:
		return 0, errSeek
	}
	if offset < 0 {
		return 0, errSeek
	}
	r.off = offset
	return offset, nil
}

func (r *SegmentReader) Close() error {
	var first error
	for _, f := range r.files {
		if err := f.Close(); err != nil && first == nil {
			first = err
		}
	}
	r.files = nil
	return first
}
