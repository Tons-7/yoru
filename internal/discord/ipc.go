// Package discord shows what Yoru plays as Discord Rich Presence, through the Discord app's local IPC pipe.
// Nothing goes to Discord's servers from here; the app on this machine does that.
package discord

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"sync/atomic"
)

// ClientID is Yoru's Discord application, which names the card and holds its images. Public by design.
const ClientID = "1561032495498268775"

// The IPC frame opcodes.
const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
)

// conn is one connection to the Discord app.
type conn struct {
	rw    io.ReadWriteCloser
	nonce atomic.Int64
}

// dial reaches the Discord app on this machine, trying each pipe it may listen on, and says hello.
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
		// Discord answers every command; read on, so its replies never fill the pipe.
		go c.drain()
		return c, nil
	}
	return nil, fmt.Errorf("discord isn't running: %w", last)
}

func (c *conn) handshake() error {
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

// setActivity shows a, or clears the card when a is nil.
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
	// The frame states its length in 32 bits.
	if uint64(len(body)) > math.MaxUint32-8 {
		return fmt.Errorf("discord: payload of %d bytes is too large", len(body))
	}
	frame := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:], op)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(body)))
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
