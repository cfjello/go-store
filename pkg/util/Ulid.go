package util

import (
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewULID generates a new ULID with timestamp and monotonic ordering
func NewULID() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// ULIDGenerator returns a function that generates ULIDs
func ULIDGenerator() func() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)

	return func() string {
		return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
	}
}
