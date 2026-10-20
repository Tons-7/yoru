package bep44

import (
	"sync"
)

var _ Store = &Memory{}

type Memory struct {
	// protects m
	mu sync.RWMutex
	m  map[Target]*Item
}

func NewMemory() *Memory {
	return &Memory{
		m: make(map[Target]*Item),
	}
}

// Puts come from anyone on the DHT; past this many, an arbitrary old item makes room.
const memoryMaxItems = 1000

func (m *Memory) Put(i *Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t := i.Target()
	if _, ok := m.m[t]; !ok && len(m.m) >= memoryMaxItems {
		for old := range m.m {
			delete(m.m, old)
			break
		}
	}
	m.m[t] = i

	return nil
}

func (m *Memory) Get(t Target) (*Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	i, ok := m.m[t]
	if !ok {
		return nil, ErrItemNotFound
	}

	return i, nil
}

func (m *Memory) Del(t Target) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.m, t)

	return nil
}
