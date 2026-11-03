package daemon

import (
	"context"
	"errors"
	"sort"
	"strings"

	"tailmulti.local/engine/internal/routing"
	"tailmulti.local/engine/internal/store"
)

// ClaimNames is an engine-internal DNS callback, never a general control API.
// An account's exact custom names cannot be reassigned after disconnect/removal.
func (m *Manager) ClaimNames(ctx context.Context, names []string, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	refused := errors.New("custom DNS ownership could not be retained")
	if m.closed || ctx.Err() != nil || m.ctx.Err() != nil || m.health != "ready" || m.plan == nil || !m.plan.ActiveProfiles[owner] || len(names) == 0 || len(names) > 16 {
		return refused
	}
	candidate := store.Clone(m.state)
	if candidate.Aliases == nil {
		candidate.Aliases = map[string]string{}
	}
	domains := map[string]bool{}
	for _, d := range candidate.Domains {
		domains[d] = true
	}
	changed := false
	for _, name := range names {
		if !routing.ValidName(name) || name != routing.Name(name) {
			return refused
		}
		if existing, known := m.plan.NameOwners[name]; known {
			if existing != owner {
				return refused
			}
			continue
		}
		if existing := candidate.Aliases[name]; existing != "" {
			if existing != owner {
				return refused
			}
			continue
		}
		// Do not shadow another tailnet's authoritative or retained DNS namespace,
		// including names beneath a previously learned exact alias.
		for d := range domains {
			if name == d || strings.HasSuffix(name, "."+d) || strings.HasSuffix(d, "."+name) {
				return refused
			}
		}
		candidate.Aliases[name] = owner
		domains[name] = true
		changed = true
	}
	if !changed {
		return nil
	}
	if len(candidate.Aliases) > 10000 {
		return refused
	}
	candidate.Domains = nil
	for d := range domains {
		candidate.Domains = append(candidate.Domains, d)
	}
	sort.Strings(candidate.Domains)
	if m.disk.Save(candidate) != nil {
		return refused
	}
	m.state = candidate
	p := *m.plan
	p.Aliases = candidate.Aliases
	p.Domains = candidate.Domains
	if m.network != nil {
		if err := m.network.Apply(&p); err != nil {
			m.failClosed("custom DNS configuration failed")
			return refused
		}
	}
	m.plan = &p
	m.dns.Set(&p)
	if ctx.Err() != nil {
		return refused
	}
	return nil
}
