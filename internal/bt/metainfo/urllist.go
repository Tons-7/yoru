package metainfo

import (
	"yoru/internal/bt/bencode"
)

type UrlList []string

var _ bencode.Unmarshaler = (*UrlList)(nil)

func (me *UrlList) UnmarshalBencode(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	if b[0] == 'l' {
		var l []any
		if bencode.Unmarshal(b, &l) == nil {
			for _, u := range l {
				if s, ok := u.(string); ok {
					*me = append(*me, s)
				}
			}
		}
		return nil
	}
	var s string
	if bencode.Unmarshal(b, &s) == nil {
		*me = []string{s}
	}
	return nil
}
