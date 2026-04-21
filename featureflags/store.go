package featureflags

import "context"

// Store is the read-side contract for a flag backend. Implementations may be
// in-memory (StaticStore, for tests) or database-backed (PostgresStore).
type Store interface {
	Get(key string) (Flag, bool)
	Refresh(ctx context.Context) error
}

// StaticStore is an in-memory Store intended for unit tests and as a
// compile-time sanity check of the Store interface. The map is not mutated
// after construction; callers seed it once and pass it by pointer.
type StaticStore struct {
	flags map[string]Flag
}

// NewStaticStore returns a StaticStore over a copy of the given flags.
func NewStaticStore(flags map[string]Flag) *StaticStore {
	cp := make(map[string]Flag, len(flags))
	for k, v := range flags {
		cp[k] = v
	}
	return &StaticStore{flags: cp}
}

func (s *StaticStore) Get(key string) (Flag, bool) {
	f, ok := s.flags[key]
	return f, ok
}

func (s *StaticStore) Refresh(_ context.Context) error { return nil }

// AllFlags returns a copy of the store's flag map. Callers may mutate the
// returned map without affecting the store.
func (s *StaticStore) AllFlags() map[string]Flag {
	cp := make(map[string]Flag, len(s.flags))
	for k, v := range s.flags {
		cp[k] = v
	}
	return cp
}

// Compile-time interface satisfaction check.
var _ Store = (*StaticStore)(nil)
