//go:build linux || windows || darwin

package tun

import (
	"net/netip"
	"testing"

	"github.com/metacubex/sing/common/control"
)

type fakeRouteInterfaceFinder struct {
	byIndex map[int]*control.Interface
}

func (f *fakeRouteInterfaceFinder) Update() error                   { return nil }
func (f *fakeRouteInterfaceFinder) Interfaces() []control.Interface { return nil }
func (f *fakeRouteInterfaceFinder) ByName(name string) (*control.Interface, error) {
	for _, ifObj := range f.byIndex {
		if ifObj.Name == name {
			return ifObj, nil
		}
	}
	return nil, ErrNoRoute
}
func (f *fakeRouteInterfaceFinder) ByIndex(index int) (*control.Interface, error) {
	if ifObj, ok := f.byIndex[index]; ok {
		return ifObj, nil
	}
	return nil, ErrNoRoute
}
func (f *fakeRouteInterfaceFinder) ByAddr(addr netip.Addr) (*control.Interface, error) {
	return nil, ErrNoRoute
}

func TestRouteTableLookup(t *testing.T) {
	tunName := "test-tun"
	entries := []routeEntry{
		// Ineligible routes must be ignored.
		{Prefix: netip.MustParsePrefix("0.0.0.0/0"), Alias: "Wi-Fi", Index: 8, Metric: 10},
		{Prefix: netip.MustParsePrefix("::/0"), Alias: "Wi-Fi", Index: 8, Metric: 10},
		{Prefix: netip.MustParsePrefix("127.0.0.0/8"), Alias: "Loopback", Index: 1, Metric: 1},
		{Prefix: netip.MustParsePrefix("::1/128"), Alias: "Loopback", Index: 1, Metric: 1},
		{Prefix: netip.MustParsePrefix("224.0.0.0/4"), Alias: "Wi-Fi", Index: 8, Metric: 1},
		{Prefix: netip.MustParsePrefix("ff00::/8"), Alias: "Wi-Fi", Index: 8, Metric: 1},
		{Prefix: netip.MustParsePrefix("255.255.255.255/32"), Alias: "Wi-Fi", Index: 8, Metric: 1},
		{Prefix: netip.MustParsePrefix("fe80::/10"), Alias: "Wi-Fi", Index: 8, Metric: 1},
		// TUN catch-all split routes that must be ignored when excludeInterface == tunName.
		{Prefix: netip.MustParsePrefix("0.0.0.0/1"), Alias: tunName, Index: 2, Metric: 0},
		{Prefix: netip.MustParsePrefix("192.0.0.0/2"), Alias: tunName, Index: 2, Metric: 0},
		// Overlapping specific routes: VPN /24 vs corporate /8, plus LAN /24.
		{Prefix: netip.MustParsePrefix("10.0.0.0/8"), Alias: "Ethernet 2", Index: 13, Metric: 50},
		{Prefix: netip.MustParsePrefix("10.10.0.0/24"), Alias: "VPN 0", Index: 4, Metric: 256},
		{Prefix: netip.MustParsePrefix("192.168.100.0/24"), Alias: "Ethernet 2", Index: 13, Metric: 35},
		{Prefix: netip.MustParsePrefix("192.168.100.0/24"), Alias: "Wi-Fi", Index: 8, Metric: 35},
		{Prefix: netip.MustParsePrefix("192.168.200.0/24"), Alias: "Wi-Fi", Index: 8, Metric: 100},
		{Prefix: netip.MustParsePrefix("192.168.200.0/24"), Alias: "Ethernet 2", Index: 13, Metric: 20},
		// Duplicate (prefix, index) keeps lowest metric.
		{Prefix: netip.MustParsePrefix("172.16.0.0/16"), Alias: "Ethernet 2", Index: 13, Metric: 100},
		{Prefix: netip.MustParsePrefix("172.16.0.0/16"), Alias: "Ethernet 2", Index: 13, Metric: 15},
		// IPv6 specific route.
		{Prefix: netip.MustParsePrefix("2001:db8:10::/48"), Alias: "VPN 0", Index: 4, Metric: 20},
	}

	table := buildRouteTable(entries, []string{"Wi-Fi", "Ethernet 2"})

	t.Run("longest prefix beats broader route with lower metric", func(t *testing.T) {
		got, ok := table.lookup(netip.MustParseAddr("10.10.0.5"), tunName)
		if !ok || got.Alias != "VPN 0" {
			t.Fatalf("got (%+v, %v), want VPN 0", got, ok)
		}

		got, ok = table.lookup(netip.MustParseAddr("10.20.3.4"), tunName)
		if !ok || got.Alias != "Ethernet 2" {
			t.Fatalf("got (%+v, %v), want Ethernet 2", got, ok)
		}

		got, ok = table.lookup(netip.MustParseAddr("10.10.0.5"), "VPN 0")
		if !ok || got.Alias != "Ethernet 2" {
			t.Fatalf("expected dynamic excludeInterface to skip VPN 0 and match Ethernet 2, got (%+v, %v)", got, ok)
		}
	})

	t.Run("excludes TUN routes and misses public IP", func(t *testing.T) {
		if got, ok := table.lookup(netip.MustParseAddr("203.0.113.1"), tunName); ok {
			t.Fatalf("expected no non-default route for 203.0.113.1 when excluding %s, got %+v", tunName, got)
		}
		if got, ok := table.lookup(netip.MustParseAddr("198.18.0.5"), tunName); ok {
			t.Fatalf("expected no route for 198.18.0.5 when excluding %s, got %+v", tunName, got)
		}
	})

	t.Run("equal prefix uses lowest metric then preferred order", func(t *testing.T) {
		got, ok := table.lookup(netip.MustParseAddr("192.168.200.5"), tunName)
		if !ok || got.Alias != "Ethernet 2" {
			t.Fatalf("expected lower metric Ethernet 2, got (%+v, %v)", got, ok)
		}

		got, ok = table.lookup(netip.MustParseAddr("192.168.100.5"), tunName)
		if !ok || got.Alias != "Wi-Fi" {
			t.Fatalf("expected equal-metric tie break to prefer Wi-Fi, got (%+v, %v)", got, ok)
		}

		got, ok = table.lookup(netip.MustParseAddr("172.16.1.2"), tunName)
		if !ok || got.Metric != 15 {
			t.Fatalf("expected deduplicated lowest metric 15, got (%+v, %v)", got, ok)
		}
	})

	t.Run("filters special addresses and handles ipv6 and 4in6", func(t *testing.T) {
		for _, raw := range []string{"0.0.0.0", "::", "127.0.0.1", "::1", "224.0.0.1", "ff02::1", "255.255.255.255", "fe80::1"} {
			if got, ok := table.lookup(netip.MustParseAddr(raw), tunName); ok {
				t.Fatalf("expected no lookup match for %s, got %+v", raw, got)
			}
		}

		got, ok := table.lookup(netip.MustParseAddr("2001:db8:10::42"), tunName)
		if !ok || got.Alias != "VPN 0" {
			t.Fatalf("expected IPv6 match VPN 0, got (%+v, %v)", got, ok)
		}

		got, ok = table.lookup(netip.MustParseAddr("::ffff:10.10.0.5"), tunName)
		if !ok || got.Alias != "VPN 0" {
			t.Fatalf("expected 4in6 unmapped match VPN 0, got (%+v, %v)", got, ok)
		}
	})
}

func TestRouteInterface(t *testing.T) {
	tunName := "test-tun"
	finder := &fakeRouteInterfaceFinder{
		byIndex: map[int]*control.Interface{
			4: {Index: 4, Name: "VPN 0"},
		},
	}
	mon := &defaultInterfaceMonitor{
		interfaceFinder: finder,
	}
	mon.routeTable.Store(buildRouteTable([]routeEntry{
		{Prefix: netip.MustParsePrefix("10.10.0.0/24"), Alias: "VPN 0", Index: 4, Metric: 256},
		{Prefix: netip.MustParsePrefix("10.20.0.0/16"), Alias: "Vanished", Index: 99, Metric: 10},
	}, nil))

	if got := mon.RouteInterface(netip.MustParseAddr("10.10.0.9"), tunName); got == nil || got.Name != "VPN 0" {
		t.Fatalf("expected VPN 0, got %+v", got)
	}
	if got := mon.RouteInterface(netip.MustParseAddr("10.20.0.9"), tunName); got != nil {
		t.Fatalf("expected nil for vanished interface index, got %+v", got)
	}
}
