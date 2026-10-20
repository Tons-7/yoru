package bt

// Port reuse may need a JS way, or dialling TCP from any local address; that fallback would want an error here.
func setReusePortSockOpts(fd uintptr) error {
	return nil
}

func setSockNoLinger(fd uintptr) error {
	return nil
}

func setSockIPTOS(fd uintptr, val int) (err error) {
	return nil
}
