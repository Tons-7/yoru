package udp

import (
	"bytes"
	"fmt"
	"net"
	"sync"
)

type Dispatcher struct {
	mu           sync.RWMutex
	transactions map[TransactionId]Transaction
}

func (me *Dispatcher) Dispatch(b []byte, addr net.Addr) error {
	buf := bytes.NewBuffer(b)
	var rh ResponseHeader
	err := Read(buf, &rh)
	if err != nil {
		return err
	}
	me.mu.RLock()
	t, ok := me.transactions[rh.TransactionId]
	me.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown transaction id %v", rh.TransactionId)
	}
	t.h(DispatchedResponse{
		Header: rh,
		Body:   append([]byte(nil), buf.Bytes()...),
		Addr:   addr,
	})
	return nil
}

func (me *Dispatcher) forgetTransaction(id TransactionId) {
	me.mu.Lock()
	defer me.mu.Unlock()
	delete(me.transactions, id)
}

func (me *Dispatcher) NewTransaction(h TransactionResponseHandler) Transaction {
	me.mu.Lock()
	defer me.mu.Unlock()
	for {
		id := RandomTransactionId()
		if _, ok := me.transactions[id]; ok {
			continue
		}
		t := Transaction{
			d:  me,
			h:  h,
			id: id,
		}
		if me.transactions == nil {
			me.transactions = make(map[TransactionId]Transaction)
		}
		me.transactions[id] = t
		return t
	}
}

type DispatchedResponse struct {
	Header ResponseHeader
	Body   []byte
	Addr   net.Addr
}
