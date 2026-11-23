package bencode

import (
	"errors"
	"io"
)

type scanner struct {
	r      io.Reader
	b      [1]byte
	unread bool
}

func (me *scanner) Read(b []byte) (int, error) {
	return me.r.Read(b)
}

func (me *scanner) ReadByte() (byte, error) {
	if me.unread {
		me.unread = false
		return me.b[0], nil
	}
	for empty := 0; ; {
		n, err := me.r.Read(me.b[:])
		switch n {
		case 0:
			if err != nil {
				return 0, err
			}
			if empty++; empty >= 100 {
				return 0, io.ErrNoProgress
			}
			continue
		case 1:
			return me.b[0], nil
		default:
			if err != nil {
				panic(err)
			}
			panic(n)
		}
	}
}

func (me *scanner) UnreadByte() error {
	if me.unread {
		return errors.New("byte already unread")
	}
	me.unread = true
	return nil
}
