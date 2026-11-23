package bt

import (
	"net"
	"time"

	"yoru/internal/bt/dht"
	"yoru/internal/bt/dht/krpc"
)

const saveDhtNodesEvery = 10 * time.Minute

func dhtServerIsIPv4(s *dht.Server) bool {
	ua, ok := s.Addr().(*net.UDPAddr)
	return ok && ua.IP.To4() != nil
}

func loadDhtNodes(file string, servers []*dht.Server) {
	ns, err := dht.ReadNodesFromFile(file)
	if err != nil {
		return
	}
	for _, s := range servers {
		v4 := dhtServerIsIPv4(s)
		for _, n := range ns {
			if (n.Addr.IP.To4() != nil) == v4 {
				s.AddNode(n)
			}
		}
	}
}

func saveDhtNodes(file string, servers []*dht.Server) {
	var ns []krpc.NodeInfo
	for _, s := range servers {
		ns = append(ns, s.Nodes()...)
	}
	if len(ns) == 0 {
		return
	}
	dht.WriteNodesToFile(ns, file)
}

func (cl *Client) keepSavingDhtNodes(file string, servers []*dht.Server) {
	tick := time.NewTicker(saveDhtNodesEvery)
	defer tick.Stop()
	for {
		select {
		case <-cl.closed.Done():
			return
		case <-tick.C:
			saveDhtNodes(file, servers)
		}
	}
}
