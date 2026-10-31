package types

import (
	"github.com/cfjello/go-store/pkg/util"
)

// SetDeps replaces the dependency list with the provided dependencies (deduplicated)
func (d *Dependencies) SetDeps(deps []string) {
	d.DependsOn = util.UniqueStrings(deps)
}

// GetDeps returns a copy of the current dependency list
func (d *Dependencies) GetDeps() []string {
	deps := make([]string, len(d.DependsOn))
	copy(deps, d.DependsOn)
	return deps
}

// AddDeps appends new dependencies, skipping duplicates
func (d *Dependencies) AddDeps(deps ...string) {
	d.DependsOn = util.UniqueStrings(append(d.DependsOn, deps...))
}

// MergeDeps merges the dependencies of another Dependencies into this one
func (d *Dependencies) MergeDeps(other Dependencies) {
	d.AddDeps(other.DependsOn...)
}

// RemoveDeps removes the specified dependencies from the dependency list
func (d *Dependencies) RemoveDeps(deps ...string) {
	remove := make(map[string]bool, len(deps))
	for _, dep := range deps {
		remove[dep] = true
	}
	kept := d.DependsOn[:0]
	for _, dep := range d.DependsOn {
		if !remove[dep] {
			kept = append(kept, dep)
		}
	}
	if len(kept) == 0 {
		d.DependsOn = nil
		return
	}
	d.DependsOn = kept
}

// ValidateDeps checks that no dependency is empty, none reference self, and each
// is registered according to the provided isValid callback
func ValidateDeps(self string, isValid func(string) bool, deps ...string) error {
	return validateKeys("dependency", "cannot depend on itself", self, isValid, deps...)
}
