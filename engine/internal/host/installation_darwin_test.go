//go:build darwin

package host

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"tailmulti.local/engine/internal/names"
)

func TestResolverDirectoryUnderPrivateServiceUmask(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "resolver")
	previous := unix.Umask(0077)
	defer unix.Umask(previous)
	if err := prepareResolverDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0755 {
		t.Fatal("system resolver cannot traverse newly created directory")
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if prepareResolverDir(dir) == nil {
		t.Fatal("private foreign resolver directory accepted")
	}
	fi, _ = os.Stat(dir)
	if fi.Mode().Perm() != 0700 {
		t.Fatal("existing directory permissions changed")
	}
}

func TestNativeServiceProvisionsOnlyItsOwnerPolicy(t *testing.T) {
	dir := t.TempDir()
	uid := uint32(os.Geteuid())
	if err := provisionOwner(dir, 501, uid); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "installation.json" {
		t.Fatal("provisioning created unexpected files")
	}
	path := filepath.Join(dir, "installation.json")
	before, _ := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal("owner policy is not private")
	}
	if err = provisionOwner(dir, 501, uid); err != nil {
		t.Fatal(err)
	}
	if err = provisionOwner(dir, 502, uid); err == nil {
		t.Fatal("another owner claimed the existing installation")
	}
	after, _ := os.ReadFile(path)
	fiAfter, _ := os.Stat(path)
	if !bytes.Equal(before, after) || fi.ModTime() != fiAfter.ModTime() {
		t.Fatal("existing policy changed")
	}
}

func TestNativeServiceRefusesUnownedOrUnsafeState(t *testing.T) {
	for _, kind := range []string{"old-state", "partial-policy", "symlink-policy", "writable-policy", "wrong-file-owner", "invalid-desktop-owner"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "installation.json")
			uid, owner := uint32(os.Geteuid()), 501
			switch kind {
			case "old-state":
				if err := os.Mkdir(filepath.Join(dir, "state"), 0700); err != nil {
					t.Fatal(err)
				}
			case "partial-policy":
				if err := os.WriteFile(path, []byte(`{"version":1`), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink-policy":
				if err := os.Symlink("missing-policy", path); err != nil {
					t.Fatal(err)
				}
			case "writable-policy", "wrong-file-owner":
				if err := provisionOwner(dir, owner, uid); err != nil {
					t.Fatal(err)
				}
				if kind == "writable-policy" {
					if err := os.Chmod(path, 0666); err != nil {
						t.Fatal(err)
					}
				} else {
					uid++
				}
			case "invalid-desktop-owner":
				owner = 0
			}
			if provisionOwner(dir, owner, uid) == nil {
				t.Fatal("unsafe provisioning accepted")
			}
			if kind == "old-state" || kind == "invalid-desktop-owner" {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("refusal still wrote an owner policy")
				}
			}
		})
	}
}

func TestInstallationOwnerIsImmutable(t *testing.T) {
	good := []byte(`{"version":1,"ownerUID":501}`)
	if err := validatePolicy(good, 501); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data  string
		owner int
	}{
		{string(good), 502}, {`{"version":1,"ownerUID":0}`, 0},
		{`{"version":2,"ownerUID":501}`, 501}, {`{"ownerUID":501}`, 501},
		{`{"version":1,"ownerUID":501,"command":"anything"}`, 501},
		{string(good) + ` {}`, 501},
	} {
		if validatePolicy([]byte(tc.data), tc.owner) == nil {
			t.Fatal("invalid installation policy accepted")
		}
	}
}

func TestServiceBindsFirstConsoleUserOnce(t *testing.T) {
	dir := t.TempDir()
	uid := uint32(os.Geteuid())
	console := 501
	asked := 0
	user := func() (int, error) { asked++; return console, nil }
	owner, err := bindOwner(dir, uid, user)
	if err != nil || owner != 501 || asked != 1 {
		t.Fatalf("first start did not bind the console user: owner %d, err %v", owner, err)
	}
	// A different user at the console later must not take over the existing installation.
	console = 502
	owner, err = bindOwner(dir, uid, user)
	if err != nil || owner != 501 || asked != 1 {
		t.Fatalf("existing owner changed: owner %d, err %v, console asked %d times", owner, err, asked)
	}
}

func TestServiceWaitsForDesktopUser(t *testing.T) {
	for _, tc := range []struct {
		name string
		uid  int
		err  error
	}{{"login window", 0, nil}, {"system account", 200, nil}, {"lookup failed", 0, os.ErrPermission}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := bindOwner(dir, uint32(os.Geteuid()), func() (int, error) { return tc.uid, tc.err }); err == nil {
				t.Fatal("service bound a non-desktop owner")
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatal("refusal wrote state")
			}
		})
	}
}

func TestServiceRejectsUnsafeExistingPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installation.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"ownerUID":501}`), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := bindOwner(dir, uint32(os.Geteuid()), func() (int, error) { return 501, nil }); err == nil {
		t.Fatal("writable owner policy accepted")
	}
}

func TestRestoreOnlyVerifiedOwnedDNS(t *testing.T) {
	dir := t.TempDir()
	if err := WriteResolvers(dir, "53", []string{"home.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "foreign.example.ts.net")
	if err := os.WriteFile(foreign, []byte("nameserver 192.0.2.53\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(dir, "linked.example.ts.net")); err != nil {
		t.Fatal(err)
	}
	if err := restoreResolvers(dir, uint32(os.Getuid())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "home.example.ts.net")); !os.IsNotExist(err) {
		t.Fatal("owned resolver retained")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("foreign resolver removed")
	}
	if _, err := os.Lstat(filepath.Join(dir, "linked.example.ts.net")); err != nil {
		t.Fatal("symlink removed")
	}
}

func TestRestoreValidatesAllEntriesBeforeRemoval(t *testing.T) {
	dir := t.TempDir()
	if err := WriteResolvers(dir, "53", []string{"a.example.ts.net", "z.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z.example.ts.net"), []byte(Header+"domain other.example.ts.net\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if restoreResolvers(dir, uint32(os.Getuid())) == nil {
		t.Fatal("modified owned resolver accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.example.ts.net")); err != nil {
		t.Fatal("partial removal before validation")
	}
}

func TestRestoreFallbackDNSPorts(t *testing.T) {
	for _, port := range names.SystemDNSPorts() {
		t.Run(strconv.Itoa(int(port)), func(t *testing.T) {
			dir := t.TempDir()
			if err := WriteResolvers(dir, strconv.Itoa(int(port)), []string{"service.example.ts.net"}); err != nil {
				t.Fatal(err)
			}
			if err := restoreResolvers(dir, uint32(os.Getuid())); err != nil {
				t.Fatal(err)
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Fatal("fallback resolver not removed", err)
			}
		})
	}
}

func TestRestoreReportsUnsafeOwnedEntries(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		mode os.FileMode
	}{
		{"oversized", Header + strings.Repeat("x", 4096), 0644},
		{"writable", Header + "domain z.example.ts.net\nnameserver 127.0.0.1\nport 53\ntimeout 1\n", 0666},
		{"unprivileged-port", Header + "domain z.example.ts.net\nnameserver 127.0.0.1\nport 1053\ntimeout 1\n", 0644},
		{"unknown-port", Header + "domain z.example.ts.net\nnameserver 127.0.0.1\nport 52\ntimeout 1\n", 0644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := WriteResolvers(dir, "53", []string{"a.example.ts.net"}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "z.example.ts.net")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			if restoreResolvers(dir, uint32(os.Getuid())) == nil {
				t.Fatal("unsafe owned file silently retained")
			}
			if _, err := os.Stat(filepath.Join(dir, "a.example.ts.net")); err != nil {
				t.Fatal("partial restoration before validation")
			}
		})
	}
}
