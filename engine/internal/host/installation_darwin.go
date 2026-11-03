//go:build darwin

package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"tailmulti.local/engine/internal/names"
	"tailmulti.local/engine/internal/routing"
)

func ValidateInstallation(owner int) error {
	if err := SecureParents(InstallRoot, 0); err != nil {
		return err
	}
	return validatePolicyFile(OwnerPolicy, owner, 0)
}

// ProvisionInstallation runs only inside the approved, root SMAppService daemon.
func ProvisionInstallation(owner int) error {
	if os.Geteuid() != 0 || owner < 501 {
		return errors.New("service provisioning requires root and a desktop owner")
	}
	if err := SecureParents(filepath.Dir(InstallRoot), 0); err != nil {
		return err
	}
	if err := os.Mkdir(InstallRoot, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := SecureParents(InstallRoot, 0); err != nil {
		return err
	}
	return provisionOwner(InstallRoot, owner, 0)
}

func provisionOwner(dir string, owner int, fileOwner uint32) error {
	if owner < 501 {
		return errors.New("invalid desktop owner")
	}
	policy := filepath.Join(dir, "installation.json")
	if _, err := os.Lstat(policy); err == nil {
		return validatePolicyFile(policy, owner, fileOwner)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("existing Tailmulti state has no owner policy; reviewed recovery is required")
	}
	data, err := json.Marshal(struct {
		Version  int `json:"version"`
		OwnerUID int `json:"ownerUID"`
	}{Version: 1, OwnerUID: owner})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".owner-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Linking publishes the complete file without replacing an existing policy.
	if err = os.Link(f.Name(), policy); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err = validatePolicyFile(policy, owner, fileOwner); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func validatePolicyFile(path string, owner int, fileOwner uint32) error {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0022 != 0 {
		return errors.New("installation policy is missing or unsafe")
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != fileOwner || fi.Size() > 4096 {
		return errors.New("installation policy has invalid ownership or size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errors.New("installation policy cannot be read")
	}
	return validatePolicy(data, owner)
}

func validatePolicy(data []byte, owner int) error {
	var p struct {
		Version  int `json:"version"`
		OwnerUID int `json:"ownerUID"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Version != 1 || p.OwnerUID <= 0 || p.OwnerUID != owner {
		return errors.New("installation owner does not match this service")
	}
	return nil
}

// RestoreResolvers removes only owned DNS overrides; cached-address guards remain.
func RestoreResolvers() error {
	if os.Geteuid() != 0 {
		return errors.New("DNS restoration requires the installed service")
	}
	if _, err := os.Lstat(ResolverDir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := SecureParents(ResolverDir, 0); err != nil {
		return err
	}
	return restoreResolvers(ResolverDir, 0)
}

func restoreResolvers(dir string, owner uint32) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var paths []string
	for _, entry := range entries {
		if !routing.ValidName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(file, 4097))
		file.Close()
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(data), Header) {
			continue
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != owner || fi.Mode().Perm()&0022 != 0 || fi.Size() > 4096 {
			return errors.New("owned DNS entry has unsafe metadata; restoration stopped")
		}
		valid := false
		for _, port := range names.SystemDNSPorts() {
			want := Header + "domain " + entry.Name() + "\nnameserver 127.0.0.1\nport " + strconv.Itoa(int(port)) + "\ntimeout 1\n"
			valid = valid || string(data) == want
		}
		if !valid {
			return errors.New("owned DNS entry has unexpected contents; restoration stopped")
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
