//go:build linux || windows || darwin

package tun

import (
	"math"
	"net/netip"
	"sort"

	"github.com/metacubex/sing/common/control"
)

type routeEntry struct {
	Prefix netip.Prefix
	Alias  string
	Index  int
	Metric uint32
}

type routeTable struct {
	v4 []routeEntry
	v6 []routeEntry
}

func addUint32Saturating(a, b uint32) uint32 {
	if a > ^uint32(0)-b {
		return ^uint32(0)
	}
	return a + b
}

func normalizeRoutePrefix(p netip.Prefix) (netip.Prefix, bool) {
	if !p.IsValid() {
		return netip.Prefix{}, false
	}
	addr := p.Addr()
	bits := p.Bits()
	if addr.Is4In6() {
		addr = addr.Unmap()
		if bits < 96 {
			return netip.Prefix{}, false
		}
		bits -= 96
	}
	norm := netip.PrefixFrom(addr, bits).Masked()
	if !norm.IsValid() {
		return netip.Prefix{}, false
	}
	return norm, true
}

func isEligibleRoutePrefix(p netip.Prefix) bool {
	norm, ok := normalizeRoutePrefix(p)
	if !ok || norm.Bits() <= 0 {
		return false
	}
	addr := norm.Addr()
	if addr.IsLoopback() || addr.IsMulticast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return false
	}
	if addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return false
	}
	if addr.Is6() && addr.IsLinkLocalUnicast() {
		return false
	}
	return true
}

func buildRouteTable(entries []routeEntry, preferred []string) *routeTable {
	type routeKey struct {
		prefix netip.Prefix
		index  int
	}
	dedup := make(map[routeKey]routeEntry, len(entries))
	for _, e := range entries {
		if e.Alias == "" || !isEligibleRoutePrefix(e.Prefix) {
			continue
		}
		norm, ok := normalizeRoutePrefix(e.Prefix)
		if !ok {
			continue
		}
		e.Prefix = norm
		k := routeKey{prefix: norm, index: e.Index}
		if prev, exists := dedup[k]; !exists || e.Metric < prev.Metric {
			dedup[k] = e
		}
	}

	preferredRank := make(map[string]int, len(preferred))
	for i, name := range preferred {
		if name == "" {
			continue
		}
		if _, exists := preferredRank[name]; !exists {
			preferredRank[name] = i
		}
	}
	rankOf := func(alias string) int {
		if r, ok := preferredRank[alias]; ok {
			return r
		}
		return math.MaxInt
	}

	table := &routeTable{}
	for _, e := range dedup {
		if e.Prefix.Addr().Is4() {
			table.v4 = append(table.v4, e)
		} else {
			table.v6 = append(table.v6, e)
		}
	}

	sortEntries := func(s []routeEntry) {
		sort.Slice(s, func(i, j int) bool {
			a, b := s[i], s[j]
			if a.Prefix.Bits() != b.Prefix.Bits() {
				return a.Prefix.Bits() > b.Prefix.Bits()
			}
			if a.Metric != b.Metric {
				return a.Metric < b.Metric
			}
			ra, rb := rankOf(a.Alias), rankOf(b.Alias)
			if ra != rb {
				return ra < rb
			}
			return a.Index < b.Index
		})
	}
	sortEntries(table.v4)
	sortEntries(table.v6)
	return table
}

func (t *routeTable) lookup(destination netip.Addr, excludeInterface string) (routeEntry, bool) {
	if t == nil || !destination.IsValid() {
		return routeEntry{}, false
	}
	addr := destination.Unmap()
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsMulticast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return routeEntry{}, false
	}
	var entries []routeEntry
	if addr.Is4() {
		entries = t.v4
	} else {
		entries = t.v6
	}
	for _, e := range entries {
		if excludeInterface != "" && e.Alias == excludeInterface {
			continue
		}
		if e.Prefix.Contains(addr) {
			return e, true
		}
	}
	return routeEntry{}, false
}

// RouteInterface returns the active outbound interface matching the longest
// non-default OS route for destination while excluding excludeInterface (e.g. TUN).
func (m *defaultInterfaceMonitor) RouteInterface(destination netip.Addr, excludeInterface string) *control.Interface {
	if m == nil {
		return nil
	}
	table := m.routeTable.Load()
	if table == nil {
		return nil
	}
	entry, ok := table.lookup(destination, excludeInterface)
	if !ok {
		return nil
	}
	if m.interfaceFinder != nil {
		iface, err := m.interfaceFinder.ByIndex(entry.Index)
		if err != nil || iface == nil {
			return nil
		}
		return iface
	}
	return &control.Interface{
		Index: entry.Index,
		Name:  entry.Alias,
	}
}
