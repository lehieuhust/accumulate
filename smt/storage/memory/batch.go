package memory

import (
	"fmt"
	"sync"

	"gitlab.com/accumulatenetwork/accumulate/smt/storage"
)

type GetFunc func(storage.Key) ([]byte, error)
type CommitFunc func(map[storage.Key][]byte) error

type Batch struct {
	get    GetFunc
	commit CommitFunc
	mu     *sync.RWMutex
	values map[storage.Key][]byte
}

func NewBatch(get GetFunc, commit CommitFunc) storage.KeyValueTxn {
	return &Batch{
		get:    get,
		commit: commit,
		mu:     new(sync.RWMutex),
		values: map[storage.Key][]byte{},
	}
}

func (db *DB) Begin(writable bool) storage.KeyValueTxn {
	b := NewBatch(db.get, db.commit)
	if db.logger == nil {
		return b
	}
	return &storage.DebugBatch{Batch: b, Logger: db.logger}
}

var _ storage.KeyValueTxn = (*Batch)(nil)

func (b *Batch) Put(key storage.Key, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values[key] = value
	return nil
}

func (b *Batch) PutAll(values map[storage.Key][]byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, v := range values {
		b.values[k] = v
	}
	return nil
}

func (b *Batch) Get(key storage.Key) (v []byte, err error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	v, ok := b.values[key]
	if ok {
		// Return a copy. Otherwise the caller could change it, and that would
		// change what's in the cache.
		u := make([]byte, len(v))
		copy(u, v)
		return u, nil
	}

	v, err = b.get(key)
	if err != nil {
		return nil, fmt.Errorf("get %v: %w", key, err)
	}
	return v, nil
}

func (b *Batch) Commit() error {
	b.mu.Lock()
	values := b.values
	b.values = nil // Prevent reuse
	b.mu.Unlock()

	return b.commit(values)
}

func (b *Batch) Discard() {
	b.mu.Lock()
	b.values = nil // Prevent reuse
	b.mu.Unlock()
}

func (b *Batch) Copy() *Batch {
	c := new(Batch)
	c.get = b.get
	c.commit = b.commit
	c.mu = new(sync.RWMutex)
	c.values = make(map[storage.Key][]byte, len(b.values))

	for k, v := range b.values {
		c.values[k] = v
	}
	return c
}