package test

import (
	"strings"
	"testing"
	"time"

	"github.com/cfjello/go-store/pkg/util"
)

// Adjust this path to match your actual project structure

// Function signatures to match the tests
var ULIDGenerator = util.ULIDGenerator
var NewULID = util.NewULID

func TestULIDGenerator(t *testing.T) {
	// Get the generator function
	generator := ULIDGenerator()

	// Generate two ULIDs
	id1 := generator()
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	id2 := generator()

	// Test that we got two different IDs
	if id1 == id2 {
		t.Errorf("Expected different ULIDs, but got the same one: %s", id1)
	}

	// Test ULID format - should be 26 characters
	if len(id1) != 26 {
		t.Errorf("Expected ULID length to be 26 characters, got %d: %s", len(id1), id1)
	}

	// Test that multiple calls maintain monotonicity
	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		ids[i] = generator()
	}

	// Check that they are ordered
	for i := 1; i < len(ids); i++ {
		if strings.Compare(ids[i-1], ids[i]) >= 0 {
			t.Errorf("ULIDs not monotonically increasing. %s should be < %s", ids[i-1], ids[i])
		}
	}
}

func TestNewULID(t *testing.T) {
	// Generate two ULIDs
	id1 := NewULID()
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	id2 := NewULID()

	// Test that we got two different IDs
	if id1 == id2 {
		t.Errorf("Expected different ULIDs, but got the same one: %s", id1)
	}

	// Test ULID format - should be 26 characters
	if len(id1) != 26 {
		t.Errorf("Expected ULID length to be 26 characters, got %d: %s", len(id1), id1)
	}
}
