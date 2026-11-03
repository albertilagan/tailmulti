// Package store owns private, crash-consistent state and a single-process lock.
package store

import (
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"tailmulti.local/engine/internal/routing"
)

type Store struct {
	Dir  string
	lock *os.File
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	// Lstat prevents a final-component symlink. Production ancestors are checked by the CLI.
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || !fi.IsDir() || fi.Mode().Perm() != 0700 {
		return nil, errors.New("state directory must be a private directory with mode 0700")
	}
	fd, err := unix.Open(filepath.Join(dir, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "lock")
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("state directory is already in use")
	}
	return &Store{Dir: dir, lock: f}, nil
}
func (s *Store) Close() error { return s.lock.Close() }
func (s *Store) Load() (routing.State, error) {
	return loadState(filepath.Join(s.Dir, "state.json"))
}
func loadState(path string) (routing.State, error) {
	state := routing.State{Version: 1, Accounts: []routing.Account{}, Domains: []string{}}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	f := os.NewFile(uintptr(fd), "state")
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return state, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0600 || fi.Size() > 32<<20 {
		return state, errors.New("invalid private state file")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 32<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&state); err != nil {
		return state, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return state, errors.New("trailing state data")
	}
	return state, routing.Validate(state)
}
func (s *Store) Save(state routing.State) error {
	if err := routing.Validate(state); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if len(state.Aliases) > 0 || len(state.IdentityIPs) > 0 {
		if err = s.preserveLegacyMetadata(); err != nil {
			return err
		}
	}
	return Atomic(filepath.Join(s.Dir, "state.json"), append(b, '\n'), 0600)
}

const LegacyMetadata = "state.before-v0.4.json"

// The immutable old inode is retained before its first schema-changing replacement.
func (s *Store) preserveLegacyMetadata() error {
	path := filepath.Join(s.Dir, "state.json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	old, err := s.Load()
	if err != nil {
		return err
	}
	if len(old.Aliases) > 0 || len(old.IdentityIPs) > 0 {
		return nil
	}
	backup := filepath.Join(s.Dir, LegacyMetadata)
	if err = os.Link(path, backup); errors.Is(err, os.ErrExist) {
		previous, e := loadState(backup)
		if e != nil || len(previous.Aliases) > 0 || len(previous.IdentityIPs) > 0 {
			return errors.New("invalid pre-upgrade metadata backup")
		}
	} else if err != nil {
		return err
	}
	d, err := os.Open(s.Dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func Atomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tailmulti-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func Clone(s routing.State) routing.State {
	b, _ := json.Marshal(s)
	var out routing.State
	_ = json.Unmarshal(b, &out)
	return out
}
