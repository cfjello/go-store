package util

import (
	"math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewULID generates a new ULID with timestamp and monotonic ordering
func NewULID() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// ULIDGenerator returns a function that generates ULIDs. The returned
// function is safe for concurrent use: rand.Rand and the monotonic entropy
// state are both guarded by the same mutex.
func ULIDGenerator() func() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	var mu sync.Mutex

	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
	}
}

var Ulid = ULIDGenerator()

