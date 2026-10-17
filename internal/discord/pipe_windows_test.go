package discord

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A write goes through while the reader waits on a silent Discord.
func TestPipeWritesWhileReading(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\yoru-test-%d`, os.Getpid())
	path, _ := windows.UTF16PtrFromString(name)
	server, err := windows.CreateNamedPipe(path, windows.PIPE_ACCESS_DUPLEX,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(server)
	connected := make(chan error, 1)
	go func() { connected <- windows.ConnectNamedPipe(server, nil) }()

	client, err := openNamedPipe(name)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := <-connected; err != nil && err != windows.ERROR_PIPE_CONNECTED {
		t.Fatal(err)
	}
	go client.Read(make([]byte, 8))
	time.Sleep(100 * time.Millisecond)

	wrote := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte("hello"))
		wrote <- err
	}()
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the write waited behind the read")
	}
	got := make([]byte, 5)
	var n uint32
	if err := windows.ReadFile(server, got, &n, nil); err != nil || string(got[:n]) != "hello" {
		t.Fatalf("server read %q, %v", got[:n], err)
	}
}
