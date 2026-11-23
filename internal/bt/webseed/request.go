package webseed

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"yoru/internal/bt/metainfo"
)

type PathEscaper func(pathComps []string) string

func defaultPathEscaper(pathComps []string) string {
	var ret []string
	for _, comp := range pathComps {
		esc := url.PathEscape(comp)
		esc = strings.ReplaceAll(esc, "+", "%2B")
		ret = append(ret, esc)
	}
	return strings.Join(ret, "/")
}

func trailingPath(
	infoName string,
	fileComps []string,
	pathEscaper PathEscaper,
) string {
	if pathEscaper == nil {
		pathEscaper = defaultPathEscaper
	}
	return pathEscaper(append([]string{infoName}, fileComps...))
}

func urlForFileIndex(
	url_ string, fileIndex int,
	info *metainfo.Info,
	pathEscaper PathEscaper,
) string {
	fileInfo := info.UpvertedFiles()[fileIndex]
	if strings.HasSuffix(url_, "/") {
		url_ += trailingPath(info.BestName(), fileInfo.BestPath(), pathEscaper)
	}
	return url_
}

func newRequest(
	ctx context.Context,
	url_ string, fileIndex int,
	info *metainfo.Info,
	offset, length int64,
	pathEscaper PathEscaper,
) (*http.Request, error) {
	fileInfo := info.UpvertedFiles()[fileIndex]
	url_ = urlForFileIndex(url_, fileIndex, info, pathEscaper)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url_, nil)
	if err != nil {
		return nil, err
	}
	if offset != 0 || length != fileInfo.Length {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	}
	return req, nil
}
