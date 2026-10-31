package types

import (
	"fmt"

	"github.com/cfjello/go-store/pkg/util"
)

// NotifyList is the list of registered object keys that must be notified when
// an object's data changes. It mirrors Dependencies with notification
// semantics: where DependsOn points downward (object -> its dependencies),
// NotifyTo points upward (object -> the objects that consume it).
type NotifyList []string

// Set replaces the notify list with the provided targets (deduplicated)
func (n *NotifyList) Set(targets []string) {
	*n = util.UniqueStrings(targets)
}

// Get returns a copy of the current notify list
func (n NotifyList) Get() []string {
	targets := make([]string, len(n))
	copy(targets, n)
	return targets
}

// Add appends new targets, skipping duplicates
func (n *NotifyList) Add(targets ...string) {
	*n = util.UniqueStrings(append(*n, targets...))
}

// Merge unions the targets of another NotifyList into this one
func (n *NotifyList) Merge(other NotifyList) {
	n.Add(other...)
}

// Remove drops the specified targets from the notify list
func (n *NotifyList) Remove(targets ...string) {
	remove := make(map[string]bool, len(targets))
	for _, target := range targets {
		remove[target] = true
	}
	kept := (*n)[:0]
	for _, target := range *n {
		if !remove[target] {
			kept = append(kept, target)
		}
	}
	if len(kept) == 0 {
		*n = nil
		return
	}
	*n = kept
}

// ValidateNotify checks that no notify target is empty, none reference self, and
// each is registered according to the provided isValid callback
func ValidateNotify(self string, isValid func(string) bool, targets ...string) error {
	return validateKeys("notify target", "cannot notify itself", self, isValid, targets...)
}

// validateKeys applies the key-list validation rules shared by ValidateDeps and
// ValidateNotify: no empty key, no self-reference, every key registered
// according to the isValid callback. label and selfErr select the wording used
// in error messages.
func validateKeys(label string, selfErr string, self string, isValid func(string) bool, keys ...string) error {
	for _, key := range keys {
		if key == "" {
			return fmt.Errorf("%s key cannot be empty", label)
		}
		if key == self {
			return fmt.Errorf("object %q %s", self, selfErr)
		}
		if !isValid(key) {
			return fmt.Errorf("%s %q is not a registered object", label, key)
		}
	}
	return nil
}
