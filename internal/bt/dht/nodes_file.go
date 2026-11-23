package dht

import (
	"io"

	"os"

	"yoru/internal/bt/dht/krpc"
)

func WriteNodesToFile(ns []krpc.NodeInfo, fileName string) (err error) {
	b, err := krpc.CompactIPv6NodeInfo(ns).MarshalBinary()
	if err != nil {
		return
	}
	tmp := fileName + ".tmp"
	if err = os.WriteFile(tmp, b, 0o640); err != nil {
		return
	}
	if err = os.Rename(tmp, fileName); err != nil {
		os.Remove(tmp)
	}
	return
}

func ReadNodesFromFile(fileName string) (ns []krpc.NodeInfo, err error) {
	f, err := os.Open(fileName)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return
	}
	var cnis krpc.CompactIPv6NodeInfo
	err = cnis.UnmarshalBinary(b)
	ns = cnis
	return
}
