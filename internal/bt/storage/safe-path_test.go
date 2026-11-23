package storage

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"testing"

	"github.com/go-quicktest/qt"

	"yoru/internal/bt/metainfo"
)

func init() {
	log.SetFlags(log.Flags() | log.Lshortfile)
}

var safeFilePathTests = []struct {
	input     []string
	expectErr bool
}{
	{input: []string{"a", filepath.FromSlash(`b/..`)}, expectErr: false},
	{input: []string{"a", filepath.FromSlash(`b/../../..`)}, expectErr: true},
	{input: []string{"a", filepath.FromSlash(`b/../.././..`)}, expectErr: true},
	{
		input: []string{
			filepath.FromSlash(`NewSuperHeroMovie-2019-English-720p.avi /../../../../../Roaming/Microsoft/Windows/Start Menu/Programs/Startup/test3.exe`),
		},
		expectErr: true,
	},
}

func TestToSafeFilePath(t *testing.T) {
	for _, _case := range safeFilePathTests {
		actual, err := ToSafeFilePath(_case.input...)
		if _case.expectErr {
			if err != nil {
				continue
			}
			t.Errorf("%q: expected error, got output %q", _case.input, actual)
		}
	}
}

func TestFileOptsSafeFilePathHandling(t *testing.T) {
	for i, _case := range safeFilePathTests {
		t.Run(fmt.Sprintf("Case%v", i), func(t *testing.T) {
			info := metainfo.Info{
				Files: []metainfo.FileInfo{
					{Path: _case.input},
				},
			}
			client := NewFileOpts(NewFileClientOpts{
				ClientBaseDir: t.TempDir(),
			})
			defer func() { qt.Assert(t, qt.IsNil(client.Close())) }()
			torImpl, err := client.OpenTorrent(context.Background(), &info, metainfo.Hash{})
			if _case.expectErr {
				qt.Check(t, qt.Not(qt.IsNil(err)))
			} else {
				qt.Check(t, qt.IsNil(torImpl.Close()))
			}
		})
	}
}
