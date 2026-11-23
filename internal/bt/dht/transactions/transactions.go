package transactions

type Key struct {
	T          Id
	RemoteAddr string
}

type Id = string
