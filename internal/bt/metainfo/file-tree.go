package metainfo

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"

	g "github.com/anacrolix/generics"

	"yoru/internal/bt/bencode"
	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

const FileTreePropertiesKey = ""

type FileTreeFile struct {
	Length     int64  `bencode:"length"`
	PiecesRoot string `bencode:"pieces root"`
}

type FileTree struct {
	File FileTreeFile
	Dir  map[string]FileTree
}

const maxFileTreeDepth = 64

func (ft *FileTree) UnmarshalBencode(bytes []byte) error {
	var v any
	if err := bencode.Unmarshal(bytes, &v); err != nil {
		return err
	}
	return ft.fromValue(v, 0)
}

func (ft *FileTree) fromValue(v any, depth int) error {
	if depth > maxFileTreeDepth {
		return errors.New("file tree is nested too deeply")
	}
	dir, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("file tree entry is a %T, not a dict", v)
	}
	if props, ok := dir[FileTreePropertiesKey]; ok {
		file, ok := props.(map[string]any)
		if !ok {
			return fmt.Errorf("file properties are a %T, not a dict", props)
		}
		if l, ok := file["length"]; ok {
			n, ok := l.(int64)
			if !ok {
				return fmt.Errorf("file length is a %T, not an integer", l)
			}
			ft.File.Length = n
		}
		if r, ok := file["pieces root"]; ok {
			s, ok := r.(string)
			if !ok {
				return fmt.Errorf("pieces root is a %T, not a string", r)
			}
			ft.File.PiecesRoot = s
		}
	}
	g.MakeMapWithCap(&ft.Dir, len(dir))
	for key, child := range dir {
		if key == FileTreePropertiesKey {
			continue
		}
		var sub FileTree
		if err := sub.fromValue(child, depth+1); err != nil {
			return err
		}
		ft.Dir[key] = sub
	}
	return nil
}

var _ bencode.Unmarshaler = (*FileTree)(nil)

func (ft *FileTree) MarshalBencode() (bytes []byte, err error) {
	if ft.IsDir() {
		dir := make(map[string]bencode.Bytes, len(ft.Dir))
		for _, key := range ft.orderedKeys() {
			sub := g.MapMustGet(ft.Dir, key)
			subBytes, err := sub.MarshalBencode()
			if err != nil {
				return nil, err
			}
			dir[key] = subBytes
		}
		return bencode.Marshal(dir)
	} else {
		fileBytes, err := bencode.Marshal(ft.File)
		if err != nil {
			return nil, err
		}
		res := map[string]bencode.Bytes{
			"": fileBytes,
		}
		return bencode.Marshal(res)
	}
}

var _ bencode.Marshaler = (*FileTree)(nil)

func (ft *FileTree) NumEntries() int {
	return len(ft.Dir)
}

func (ft *FileTree) IsDir() bool {
	return ft.NumEntries() != 0
}

func (ft *FileTree) orderedKeys() []string {
	return slices.Sorted(maps.Keys(ft.Dir))
}

func (ft *FileTree) upvertedFiles(pieceLength int64) iter.Seq[FileInfo] {
	var offset int64
	return ft.upvertedFilesInner(pieceLength, nil, &offset)
}

func (ft *FileTree) upvertedFilesInner(
	pieceLength int64,
	path []string,
	offset *int64,
) iter.Seq[FileInfo] {
	return func(yield func(FileInfo) bool) {
		if ft.IsDir() {
			for _, key := range ft.orderedKeys() {
				sub := g.MapMustGet(ft.Dir, key)
				for fi := range sub.upvertedFilesInner(pieceLength, append(path, key), offset) {
					if !yield(fi) {
						return
					}
				}
			}
		} else {
			yield(FileInfo{
				Length:        ft.File.Length,
				Path:          append([]string(nil), path...),
				PathUtf8:      append([]string(nil), path...),
				PiecesRoot:    ft.PiecesRootAsByteArray(),
				TorrentOffset: *offset,
			})
			*offset += (ft.File.Length + pieceLength - 1) / pieceLength * pieceLength
		}

	}
}

func (ft *FileTree) Walk(path []string, f func(path []string, ft *FileTree)) {
	f(path, ft)
	for key, sub := range ft.Dir {
		sub.Walk(append(path, key), f)
	}
}

func (ft *FileTree) PiecesRootAsByteArray() (ret g.Option[infohash_v2.T]) {
	if ft.File.PiecesRoot == "" {
		return
	}
	n := copy(ret.Value[:], ft.File.PiecesRoot)
	if n != 32 {
		panic(n)
	}
	ret.Ok = true
	return
}
