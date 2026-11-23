package indexer

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	KindNyaa       = "nyaa"
	KindTokyoTosho = "tokyotosho"
)

func Build(kind, base string, adult bool) (Source, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
		return nil, fmt.Errorf("indexer %q: url %q is not a full http(s) address", kind, base)
	}
	switch strings.ToLower(kind) {
	case KindNyaa:
		if adult {
			return NewSukebei(base), nil
		}
		return NewNyaa(base), nil
	case KindTokyoTosho:
		if adult {
			return nil, fmt.Errorf("indexer %q: adult is only supported for %q sites", kind, KindNyaa)
		}
		return NewTokyoTosho(base), nil
	}
	return nil, fmt.Errorf("indexer type %q: want %q or %q", kind, KindNyaa, KindTokyoTosho)
}
