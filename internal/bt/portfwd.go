package bt

import (
	"fmt"
	"sync"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/upnp"
)

const UpnpDiscoverLogTag = "upnp-discover"

type upnpMapping struct {
	d            upnp.Device
	proto        upnp.Protocol
	externalPort int
}

func (cl *Client) addPortMapping(d upnp.Device, proto upnp.Protocol, internalPort int, upnpID string) {
	logger := cl.logger.WithContextText(fmt.Sprintf("UPnP device at %v: mapping internal %v port %v", d.GetLocalIPAddress(), proto, internalPort))
	externalPort, err := d.AddPortMapping(proto, internalPort, internalPort, upnpID, 0)
	if f := cl.config.UpnpMapped; f != nil {
		f(string(proto), internalPort, externalPort, err)
	}
	if err != nil {
		logger.WithDefaultLevel(log.Warning).Printf("error: %v", err)
		return
	}
	cl.lock()
	// Discovery is slow; a client closed meanwhile has already cleared its list, so undo this one now.
	if cl.closed.IsSet() {
		cl.unlock()
		cl.deletePortMapping(d, proto, externalPort)
		return
	}
	cl.upnpMappings = append(cl.upnpMappings, &upnpMapping{d, proto, externalPort})
	cl.unlock()
	level := log.Info
	if externalPort != internalPort {
		level = log.Warning
	}
	logger.WithDefaultLevel(level).Printf("success: external port %v", externalPort)
}

func (cl *Client) forwardPort() {
	ds := upnp.Discover(0, 2*time.Second, cl.logger.WithValues(UpnpDiscoverLogTag))
	cl.lock()
	cl.logger.WithDefaultLevel(log.Debug).Printf("discovered %d upnp devices", len(ds))
	port := cl.incomingPeerPort()
	id := cl.config.UpnpID
	cl.unlock()
	if f := cl.config.UpnpDiscovered; f != nil {
		f(len(ds))
	}
	for _, d := range ds {
		go cl.addPortMapping(d, upnp.TCP, port, id)
		go cl.addPortMapping(d, upnp.UDP, port, id)
	}
}

func (cl *Client) deletePortMapping(d upnp.Device, proto upnp.Protocol, externalPort int) {
	logger := cl.logger.WithContextText(fmt.Sprintf("UPnP device at %v: delete mapping internal %v port %v", d.GetLocalIPAddress(), proto, externalPort))
	err := d.DeletePortMapping(proto, externalPort)
	if err != nil {
		logger.WithDefaultLevel(log.Warning).Printf("error: %v", err)
		return
	}
}

// clearPortMappings starts removing the router mappings; the caller waits on the result outside the
// client lock, or an exit straight after Close cuts the requests off and the mappings stay.
func (cl *Client) clearPortMappings() *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, m := range cl.upnpMappings {
		wg.Go(func() { cl.deletePortMapping(m.d, m.proto, m.externalPort) })
	}
	cl.upnpMappings = nil
	return &wg
}

// waitBriefly waits for wg, but no longer than d: a router that doesn't answer mustn't hold up exit.
func waitBriefly(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}
