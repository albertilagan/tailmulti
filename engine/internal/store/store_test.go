package store

import (
	"os"
	"path/filepath"
	"tailmulti.local/engine/internal/routing"
	"testing"
)

func TestPrivateAtomicPersistenceAndLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(dir); err == nil {
		t.Fatal("duplicate writer")
	}
	want := routing.State{Version: 1, Accounts: []routing.Account{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Home"}}}
	if err = s.Save(want); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "state.json"))
	if fi.Mode().Perm() != 0600 {
		t.Fatal("state permissions")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Load()
	if err != nil || got.Accounts[0].Name != "Home" {
		t.Fatal("restart load", err)
	}
}
func TestRejectSymlinkAndCorruptState(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	os.Symlink("/etc/passwd", filepath.Join(dir, "state.json"))
	if _, err = s.Load(); err == nil {
		t.Fatal("read symlink")
	}
	os.Remove(filepath.Join(dir, "state.json"))
	os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"version":99}`), 0600)
	if _, err = s.Load(); err == nil {
		t.Fatal("corrupt version")
	}
}
