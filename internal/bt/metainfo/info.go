package metainfo

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"yoru/internal/bt/segments"
)

type Info struct {
	PieceLength int64  `bencode:"piece length"`
	Pieces      []byte `bencode:"pieces,omitempty"`
	Name        string `bencode:"name"`
	NameUtf8    string `bencode:"name.utf-8,omitempty"`
	Length      int64  `bencode:"length,omitempty"`
	ExtendedFileAttrs
	Private *bool      `bencode:"private,omitempty"`
	Source  string     `bencode:"source,omitempty"`
	Files   []FileInfo `bencode:"files,omitempty"`

	MetaVersion int64    `bencode:"meta version,omitempty"`
	FileTree    FileTree `bencode:"file tree,omitempty"`
}

const NoName = "-"

func (info *Info) BuildFromFilePath(root string) (err error) {
	info.Name = func() string {
		b := filepath.Base(root)
		switch b {
		case ".", "..", string(filepath.Separator):
			return NoName
		default:
			return b
		}
	}()
	info.Files = nil
	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		} else if path == root {
			info.Length = fi.Size()
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("error getting relative path: %s", err)
		}
		info.Files = append(info.Files, FileInfo{
			Path:   strings.Split(relPath, string(filepath.Separator)),
			Length: fi.Size(),
		})
		return nil
	})
	if err != nil {
		return
	}
	sort.Slice(info.Files, func(i, j int) bool {
		l, r := info.Files[i], info.Files[j]
		return strings.Join(l.BestPath(), "/") < strings.Join(r.BestPath(), "/")
	})
	if info.PieceLength == 0 {
		info.PieceLength = ChoosePieceLength(info.TotalLength())
	}
	err = info.GeneratePieces(func(fi FileInfo) (io.ReadCloser, error) {
		return os.Open(filepath.Join(root, strings.Join(fi.BestPath(), string(filepath.Separator))))
	})
	if err != nil {
		err = fmt.Errorf("error generating pieces: %s", err)
	}
	return
}

func (info *Info) writeFiles(w io.Writer, open func(fi FileInfo) (io.ReadCloser, error)) error {
	for _, fi := range info.UpvertedFiles() {
		r, err := open(fi)
		if err != nil {
			return fmt.Errorf("error opening %v: %s", fi, err)
		}
		wn, err := io.CopyN(w, r, fi.Length)
		r.Close()
		if wn != fi.Length {
			return fmt.Errorf("error copying %v: %s", fi, err)
		}
	}
	return nil
}

func (info *Info) GeneratePieces(open func(fi FileInfo) (io.ReadCloser, error)) (err error) {
	if info.PieceLength == 0 {
		return errors.New("piece length must be non-zero")
	}
	pr, pw := io.Pipe()
	go func() {
		err := info.writeFiles(pw, open)
		pw.CloseWithError(err)
	}()
	defer pr.Close()
	info.Pieces, err = GeneratePieces(pr, info.PieceLength, nil)
	return
}

func (info *Info) TotalLength() (ret int64) {
	for _, fi := range info.UpvertedFiles() {
		ret += fi.Length
	}
	return
}

func (info *Info) NumPieces() (num int) {
	if info.HasV2() {
		info.FileTree.Walk(nil, func(path []string, ft *FileTree) {
			if !ft.IsDir() {
				num += int((ft.File.Length + info.PieceLength - 1) / info.PieceLength)
			}
		})
		return
	}
	return len(info.Pieces) / 20
}

func (info *Info) IsDir() bool {
	if info.HasV2() {
		return info.FileTree.IsDir()
	}
	return len(info.Files) != 0
}

func (info *Info) UpvertedFiles() (files []FileInfo) {
	return slices.Collect(info.UpvertedFilesIter())
}

func (info *Info) UpvertedFilesIter() iter.Seq[FileInfo] {
	if info.HasV2() {
		return info.FileTree.upvertedFiles(info.PieceLength)
	}
	return info.UpvertedV1Files()
}

func (info *Info) UpvertedV1Files() iter.Seq[FileInfo] {
	return func(yield func(FileInfo) bool) {
		if len(info.Files) == 0 {
			yield(FileInfo{
				Length: info.Length,
				Path:   nil,
			})
			return
		}
		var offset int64
		for _, fi := range info.Files {
			fi.TorrentOffset = offset
			offset += fi.Length
			if !yield(fi) {
				return
			}
		}
	}
}

func (info *Info) Piece(index int) Piece {
	return Piece{info, index}
}

func (info *Info) BestName() string {
	if info.NameUtf8 != "" {
		return info.NameUtf8
	}
	return info.Name
}

func (info *Info) HasV2() bool {
	return info.MetaVersion == 2
}

func (info *Info) HasV1() bool {
	return info.MetaVersion == 0 || info.MetaVersion == 1 || info.Files != nil || info.Length != 0 || len(info.Pieces) != 0
}

func (info *Info) FilesArePieceAligned() bool {
	return info.HasV2()
}

func (info *Info) FileSegmentsIndex() segments.Index {
	return segments.NewIndexFromSegments(slices.Collect(func(yield func(segments.Extent) bool) {
		for fi := range info.UpvertedFilesIter() {
			yield(segments.Extent{Start: fi.TorrentOffset, Length: fi.Length})
		}
	}))
}
