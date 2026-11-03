package store

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"tailmulti.local/engine/internal/routing"
)

func TestDNSMetadataBackupBeforeSchemaChange(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := routing.State{Version: 1, Accounts: []routing.Account{{ID: id, Name: "Home"}}, Domains: []string{"home.ts.net"}}
	if s.Save(state) != nil {
		t.Fatal("legacy save")
	}
	original, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state.IdentityIPs = map[string][]netip.Addr{id: {netip.MustParseAddr("100.64.0.1")}}
	if err = s.Save(state); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(s.Dir, LegacyMetadata)
	saved, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(original, saved) {
		t.Fatal("legacy metadata not preserved")
	}
	fi, err := os.Stat(backup)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatal("backup is not private")
	}
	state.Aliases = map[string]string{"app.example.com": id}
	if err = s.Save(state); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(backup)
	if !bytes.Equal(original, again) {
		t.Fatal("backup overwritten")
	}
	current, err := s.Load()
	if err != nil || current.Aliases["app.example.com"] != id {
		t.Fatal("new state not saved")
	}
}

func TestUnsafeBackupPreventsSchemaChange(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	state := routing.State{Version: 1, Accounts: []routing.Account{{ID: id, Name: "Home"}}}
	if s.Save(state) != nil {
		t.Fatal("legacy save")
	}
	original, _ := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if os.Symlink("state.json", filepath.Join(s.Dir, LegacyMetadata)) != nil {
		t.Fatal("fixture")
	}
	state.Aliases = map[string]string{"app.example.com": id}
	if s.Save(state) == nil {
		t.Fatal("unsafe backup accepted")
	}
	current, _ := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if !bytes.Equal(original, current) {
		t.Fatal("state upgraded without backup")
	}
}
