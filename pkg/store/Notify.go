package store

import (
	"errors"
	"fmt"
	"log"

	"github.com/cfjello/go-store/pkg/perf"
	"github.com/cfjello/go-store/pkg/types"
)

// Notifier receives change notifications for registered objects: after a
// successful Set or Publish, the store calls Notify once for every object
// named in the changed object's notify list. The job package registers its
// JobService as the store's Notifier via SetNotifier, which lets the store
// trigger job flows without importing the job package (an import cycle).
type Notifier interface {
	Notify(key string) error
}

// SetNotifier registers the notifier that receives change notifications.
// Call once during setup, before serving traffic; passing nil disables
// notifications.
func (s *Store) SetNotifier(n Notifier) {
	s.notifier = n
}

// notifyChange emits a change notification to every object named in the
// metadata's notify list. It runs after the data write succeeded; errors are
// logged, never returned, so a failed notification cannot misreport a
// successful write.
func (s *Store) notifyChange(meta types.MetaData) {
	defer perf.Track("store.notifyChange")()
	if s.notifier == nil {
		return
	}
	for _, target := range meta.NotifyTo {
		if err := s.notifier.Notify(target); err != nil {
			log.Printf("failed to notify %q of change to %q: %v", target, meta.Key, err)
		}
	}
}

// SetNotify replaces the notify list of the object identified by key with
// targets. Every target must be a registered object other than key itself.
func (s *Store) SetNotify(key string, targets []string) error {
	return s.withMetaNotify(key, targets, func(n *types.NotifyList) {
		n.Set(targets)
	})
}

// GetNotify returns the notify list of the object identified by key
func (s *Store) GetNotify(key string) ([]string, error) {
	if key == "" {
		return nil, errors.New("the key cannot be empty")
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return nil, fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	return meta.NotifyTo.Get(), nil
}

// AddNotify appends targets to the notify list of the object identified by key
func (s *Store) AddNotify(key string, targets ...string) error {
	return s.withMetaNotify(key, targets, func(n *types.NotifyList) {
		n.Add(targets...)
	})
}

// RemoveNotify removes targets from the notify list of the object identified
// by key
func (s *Store) RemoveNotify(key string, targets ...string) error {
	if key == "" {
		return errors.New("the key cannot be empty")
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	meta.NotifyTo.Remove(targets...)
	if !s.SetMeta(key, meta) {
		return fmt.Errorf("failed to update notify list for %q", key)
	}
	return nil
}

// withMetaNotify validates the notify targets, fetches the metadata for key,
// applies mutate to its NotifyList, and persists the result. It mirrors
// withMetaDeps; the isValid callback used for target registration checks is
// s.IsRegistered.
func (s *Store) withMetaNotify(key string, targets []string, mutate func(*types.NotifyList)) error {
	if key == "" {
		return errors.New("the key cannot be empty")
	}
	if err := types.ValidateNotify(key, s.IsRegistered, targets...); err != nil {
		return err
	}
	meta, err := s.GetMeta(key)
	if err != nil {
		return fmt.Errorf("cannot get metadata for %q: %v", key, err)
	}
	mutate(&meta.NotifyTo)
	if !s.SetMeta(key, meta) {
		return fmt.Errorf("failed to update notify list for %q", key)
	}
	return nil
}
