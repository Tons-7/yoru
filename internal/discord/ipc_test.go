package discord

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"testing"
)

// fakeDiscord answers like the Discord app on the far end of the pipe and hands back what it was sent.
func fakeDiscord(t *testing.T, side net.Conn, frames chan<- map[string]any) {
	t.Helper()
	readFrame := func() (uint32, map[string]any) {
		var header [8]byte
		if _, err := io.ReadFull(side, header[:]); err != nil {
			return 0, nil
		}
		body := make([]byte, binary.LittleEndian.Uint32(header[4:]))
		io.ReadFull(side, body)
		var m map[string]any
		json.Unmarshal(body, &m)
		return binary.LittleEndian.Uint32(header[:4]), m
	}
	writeFrame := func(op uint32, v any) {
		body, _ := json.Marshal(v)
		var header [8]byte
		binary.LittleEndian.PutUint32(header[:4], op)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(body)))
		side.Write(append(header[:], body...))
	}
	go func() {
		op, hello := readFrame()
		hello["op"] = float64(op)
		frames <- hello
		writeFrame(opFrame, map[string]any{"cmd": "DISPATCH", "evt": "READY"})
		for {
			op, m := readFrame()
			if m == nil {
				close(frames)
				return
			}
			m["op"] = float64(op)
			frames <- m
			writeFrame(opFrame, map[string]any{"cmd": "SET_ACTIVITY", "nonce": m["nonce"]})
		}
	}()
}

// Yoru speaks Discord's IPC: a handshake with its application id, then SET_ACTIVITY frames, replies drained.
func TestIPCFrames(t *testing.T) {
	ours, theirs := net.Pipe()
	frames := make(chan map[string]any, 8)
	fakeDiscord(t, theirs, frames)

	c := &conn{rw: ours}
	if err := c.handshake(); err != nil {
		t.Fatal(err)
	}
	hello := <-frames
	if hello["op"] != float64(opHandshake) || hello["client_id"] != ClientID || hello["v"] != float64(1) {
		t.Errorf("handshake %v", hello)
	}
	go c.drain()

	card := card{w: Watching{Title: "Frieren", Episode: "Episode 5 of 28", Duration: 1440}}
	if err := c.setActivity(card.activity()); err != nil {
		t.Fatal(err)
	}
	got := <-frames
	args, _ := got["args"].(map[string]any)
	activity, _ := args["activity"].(map[string]any)
	if got["op"] != float64(opFrame) || got["cmd"] != "SET_ACTIVITY" || activity["details"] != "Frieren" || args["pid"] == nil {
		t.Errorf("activity frame %v", got)
	}
	if err := c.setActivity(nil); err != nil {
		t.Fatal(err)
	}
	cleared := <-frames
	if a := cleared["args"].(map[string]any); a["activity"] != nil {
		t.Errorf("clearing sent %v", a["activity"])
	}
	c.Close()
}
