package bt

import "net"

type StringAddr string

var _ net.Addr = StringAddr("")

func (StringAddr) Network() string   { return "" }
func (me StringAddr) String() string { return string(me) }
