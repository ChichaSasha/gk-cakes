package recovery

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound means the journal has no object with that name.
var ErrNotFound = errors.New("recovery: journal object not found")

// Journal is the independent store for recovery envelopes and essential
// blob copies. Put returns only after the store has durably accepted the exact
// bytes (its acknowledgment); objects are never deleted through it.
type Journal interface {
	Put(ctx context.Context, name string, data []byte) error
	Get(ctx context.Context, name string) ([]byte, error)
	// List returns all object names under prefix in lexical order.
	List(ctx context.Context, prefix string) ([]string, error)
	// Ping checks that the journal is reachable and correctly scoped.
	Ping(ctx context.Context) error
}

// MemoryJournal is an in-process Journal for tests. Fail, when set, is
// consulted before every operation and can inject outages.
type MemoryJournal struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    map[string]int
	Fail    func(op, name string) error
}

// NewMemoryJournal returns an empty MemoryJournal.
func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{objects: map[string][]byte{}, puts: map[string]int{}}
}

func (m *MemoryJournal) fail(op, name string) error {
	if m.Fail == nil {
		return nil
	}
	return m.Fail(op, name)
}

// Put implements Journal.
func (m *MemoryJournal) Put(_ context.Context, name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("put", name); err != nil {
		return err
	}
	m.objects[name] = append([]byte(nil), data...)
	m.puts[name]++
	return nil
}

// Get implements Journal.
func (m *MemoryJournal) Get(_ context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("get", name); err != nil {
		return nil, err
	}
	data, ok := m.objects[name]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

// List implements Journal.
func (m *MemoryJournal) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("list", prefix); err != nil {
		return nil, err
	}
	var names []string
	for name := range m.objects {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Ping implements Journal.
func (m *MemoryJournal) Ping(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fail("ping", "")
}

// Puts reports how many times name was written (tests check write-once use).
func (m *MemoryJournal) Puts(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.puts[name]
}

// Names returns every stored object name in lexical order, ignoring Fail.
func (m *MemoryJournal) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.objects))
	for name := range m.objects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
