//go:build linux || windows || darwin

package tun

import "testing"

func TestSelectDefaultInterface(t *testing.T) {
	primary := defaultRouteCandidate{Alias: "Wi-Fi", Index: 1, Metric: 55}
	secondary := defaultRouteCandidate{Alias: "Ethernet 2", Index: 2, Metric: 36}
	other := defaultRouteCandidate{Alias: "Ethernet", Index: 3, Metric: 15}

	t.Run("preferred overrides lower metric", func(t *testing.T) {
		got, ok := selectDefaultInterface([]defaultRouteCandidate{secondary, primary}, []string{"Wi-Fi", "Ethernet 2"}, "Ethernet 2")
		if !ok || got.Alias != "Wi-Fi" {
			t.Fatalf("got (%+v, %v), want Wi-Fi", got, ok)
		}
	})

	t.Run("failover when primary absent", func(t *testing.T) {
		got, ok := selectDefaultInterface([]defaultRouteCandidate{other, secondary}, []string{"Wi-Fi", "Ethernet 2"}, "Wi-Fi")
		if !ok || got.Alias != "Ethernet 2" {
			t.Fatalf("got (%+v, %v), want Ethernet 2", got, ok)
		}
	})

	t.Run("failback when primary returns", func(t *testing.T) {
		got, ok := selectDefaultInterface([]defaultRouteCandidate{secondary, primary}, []string{"Wi-Fi", "Ethernet 2"}, "Ethernet 2")
		if !ok || got.Alias != "Wi-Fi" {
			t.Fatalf("got (%+v, %v), want Wi-Fi", got, ok)
		}
	})

	t.Run("fallback to lowest metric when no preferred connected", func(t *testing.T) {
		got, ok := selectDefaultInterface([]defaultRouteCandidate{secondary, other}, []string{"Wi-Fi"}, "")
		if !ok || got.Alias != "Ethernet" {
			t.Fatalf("got (%+v, %v), want Ethernet", got, ok)
		}
	})

	t.Run("tie-break keeps current interface", func(t *testing.T) {
		c1 := defaultRouteCandidate{Alias: "Ethernet 2", Index: 2, Metric: 35}
		c2 := defaultRouteCandidate{Alias: "Wi-Fi", Index: 1, Metric: 35}
		got, ok := selectDefaultInterface([]defaultRouteCandidate{c1, c2}, nil, "Wi-Fi")
		if !ok || got.Alias != "Wi-Fi" {
			t.Fatalf("got (%+v, %v), want Wi-Fi", got, ok)
		}
	})

	t.Run("empty candidates", func(t *testing.T) {
		if _, ok := selectDefaultInterface(nil, []string{"Wi-Fi"}, ""); ok {
			t.Fatal("expected ok=false for empty candidates")
		}
	})

	t.Run("selector deduplicates per index using lowest metric", func(t *testing.T) {
		var s defaultRouteSelector
		s.add("Wi-Fi", 1, 80)
		s.add("Wi-Fi", 1, 25)
		s.add("Ethernet 2", 2, 40)

		alias, idx, ok := s.selectFor(nil)
		if !ok || alias != "Wi-Fi" || idx != 1 {
			t.Fatalf("got (%q, %d, %v), want (\"Wi-Fi\", 1, true)", alias, idx, ok)
		}
	})

	t.Run("linux preferred overrides lower netlink metric", func(t *testing.T) {
		eth := defaultRouteCandidate{Alias: "eth0", Index: 2, Metric: 100}
		wlan := defaultRouteCandidate{Alias: "wlan0", Index: 3, Metric: 600}

		got, ok := selectDefaultInterface([]defaultRouteCandidate{eth, wlan}, []string{"wlan0", "eth0"}, "eth0")
		if !ok || got.Alias != "wlan0" {
			t.Fatalf("got (%+v, %v), want wlan0", got, ok)
		}

		got, ok = selectDefaultInterface([]defaultRouteCandidate{eth, wlan}, nil, "")
		if !ok || got.Alias != "eth0" {
			t.Fatalf("got (%+v, %v), want eth0 when preferred is nil", got, ok)
		}
	})

	t.Run("darwin scoped preferred overrides unscoped primary", func(t *testing.T) {
		en7 := defaultRouteCandidate{Alias: "en7", Index: 7, Metric: 0}
		en0 := defaultRouteCandidate{Alias: "en0", Index: 4, Metric: (1 << 30) + 1}

		got, ok := selectDefaultInterface([]defaultRouteCandidate{en7, en0}, []string{"en0", "en7"}, "en7")
		if !ok || got.Alias != "en0" {
			t.Fatalf("got (%+v, %v), want en0", got, ok)
		}

		got, ok = selectDefaultInterface([]defaultRouteCandidate{en7}, []string{"en0", "en7"}, "en0")
		if !ok || got.Alias != "en7" {
			t.Fatalf("got (%+v, %v), want en7 when en0 absent", got, ok)
		}
	})

	t.Run("darwin unscoped beats scoped when preferred unset", func(t *testing.T) {
		en0 := defaultRouteCandidate{Alias: "en0", Index: 4, Metric: 1 << 30}
		en7 := defaultRouteCandidate{Alias: "en7", Index: 7, Metric: 1}

		got, ok := selectDefaultInterface([]defaultRouteCandidate{en0, en7}, nil, "en0")
		if !ok || got.Alias != "en7" {
			t.Fatalf("got (%+v, %v), want en7", got, ok)
		}
	})
}
