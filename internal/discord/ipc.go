package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

const ClientID = "1561032495498268775"

const ipcWait = 5 * time.Second

const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
)

const maxPayload = 64 << 10

type conn struct {
	rw    io.ReadWriteCloser
	nonce atomic.Int64
}

func dial() (*conn, error) {
	var last error
	for i := range 10 {
		rw, err := openPipe(i)
		if err != nil {
			last = err
			continue
		}
		c := &conn{rw: rw}
		if err := c.handshake(); err != nil {
			rw.Close()
			last = err
			continue
		}
		go c.drain()
		return c, nil
	}
	return nil, fmt.Errorf("discord isn't running: %w", last)
}

func (c *conn) closeAfterWait() *time.Timer {
	return time.AfterFunc(ipcWait, func() { c.rw.Close() })
}

func (c *conn) handshake() error {
	defer c.closeAfterWait().Stop()
	if err := c.write(opHandshake, map[string]any{"v": 1, "client_id": ClientID}); err != nil {
		return err
	}
	op, body, err := c.read()
	if err != nil {
		return err
	}
	if op == opClose {
		return fmt.Errorf("discord closed the connection: %s", body)
	}
	var ready struct {
		Evt string `json:"evt"`
	}
	if json.Unmarshal(body, &ready) != nil || ready.Evt != "READY" {
		return fmt.Errorf("discord answered %s", body)
	}
	return nil
}

func (c *conn) setActivity(a any) error {
	return c.write(opFrame, map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  map[string]any{"pid": os.Getpid(), "activity": a},
		"nonce": strconv.FormatInt(c.nonce.Add(1), 10),
	})
}

func (c *conn) write(op uint32, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(body) > maxPayload {
		return fmt.Errorf("discord: payload of %d bytes is too large", len(body))
	}
	frame := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:], op)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(body)))
	defer c.closeAfterWait().Stop()
	_, err = c.rw.Write(append(frame, body...))
	return err
}

func (c *conn) read() (uint32, []byte, error) {
	var header [8]byte
	if _, err := io.ReadFull(c.rw, header[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(header[4:])
	if n > 1<<20 {
		return 0, nil, errors.New("discord sent an oversized frame")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(c.rw, body)
	return binary.LittleEndian.Uint32(header[:4]), body, err
}

func (c *conn) drain() {
	for {
		if _, _, err := c.read(); err != nil {
			return
		}
	}
}

func (c *conn) Close() error { return c.rw.Close() }
