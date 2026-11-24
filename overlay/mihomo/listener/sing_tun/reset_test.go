package sing_tun

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/tunnel/statistic"
	tun "github.com/metacubex/sing-tun"
	"github.com/metacubex/sing/common/control"
	"github.com/metacubex/sing/common/x/list"
)

type fakePlainAdapter struct {
	name  string
	iface string
}

func (f *fakePlainAdapter) Name() string                 { return f.name }
func (f *fakePlainAdapter) Type() C.AdapterType          { return C.Direct }
func (f *fakePlainAdapter) Addr() string                 { return "" }
func (f *fakePlainAdapter) SupportUDP() bool             { return true }
func (f *fakePlainAdapter) ProxyInfo() C.ProxyInfo       { return C.ProxyInfo{Interface: f.iface} }
func (f *fakePlainAdapter) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }
func (f *fakePlainAdapter) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, nil
}
func (f *fakePlainAdapter) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, nil
}
func (f *fakePlainAdapter) SupportUOT() bool                 { return false }
func (f *fakePlainAdapter) IsL3Protocol(*C.Metadata) bool    { return false }
func (f *fakePlainAdapter) Unwrap(*C.Metadata, bool) C.Proxy { return nil }
func (f *fakePlainAdapter) Close() error                     { return nil }

type fakeAdapter struct {
	fakePlainAdapter
	resetHits int
}

func (f *fakeAdapter) ResetSessions() { f.resetHits++ }

type fakeUpstreamWrapper struct {
	C.ProxyAdapter
}

func (w *fakeUpstreamWrapper) Upstream() any { return w.ProxyAdapter }

type fakeProxy struct {
	adapter C.ProxyAdapter
}

func (p *fakeProxy) Name() string                 { return p.adapter.Name() }
func (p *fakeProxy) Type() C.AdapterType          { return p.adapter.Type() }
func (p *fakeProxy) Addr() string                 { return p.adapter.Addr() }
func (p *fakeProxy) SupportUDP() bool             { return p.adapter.SupportUDP() }
func (p *fakeProxy) ProxyInfo() C.ProxyInfo       { return p.adapter.ProxyInfo() }
func (p *fakeProxy) MarshalJSON() ([]byte, error) { return p.adapter.MarshalJSON() }
func (p *fakeProxy) DialContext(ctx context.Context, m *C.Metadata) (C.Conn, error) {
	return p.adapter.DialContext(ctx, m)
}
func (p *fakeProxy) ListenPacketContext(ctx context.Context, m *C.Metadata) (C.PacketConn, error) {
	return p.adapter.ListenPacketContext(ctx, m)
}
func (p *fakeProxy) SupportUOT() bool                         { return p.adapter.SupportUOT() }
func (p *fakeProxy) IsL3Protocol(m *C.Metadata) bool          { return p.adapter.IsL3Protocol(m) }
func (p *fakeProxy) Unwrap(m *C.Metadata, touch bool) C.Proxy { return p.adapter.Unwrap(m, touch) }
func (p *fakeProxy) Close() error                             { return p.adapter.Close() }
func (p *fakeProxy) Adapter() C.ProxyAdapter                  { return p.adapter }
func (p *fakeProxy) AliveForTestUrl(string) bool              { return true }
func (p *fakeProxy) DelayHistory() []C.DelayHistory           { return nil }
func (p *fakeProxy) ExtraDelayHistories() map[string]C.ProxyState {
	return nil
}
func (p *fakeProxy) LastDelayForTestUrl(string) uint16 { return 0 }
func (p *fakeProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 0, nil
}

type fakeTracker struct {
	info   *statistic.TrackerInfo
	closed bool
}

func (t *fakeTracker) ID() string                    { return t.info.UUID.String() }
func (t *fakeTracker) Close() error                  { t.closed = true; return nil }
func (t *fakeTracker) Info() *statistic.TrackerInfo  { return t.info }
func (t *fakeTracker) Chains() C.Chain               { return t.info.Chain }
func (t *fakeTracker) ProviderChains() C.Chain       { return t.info.ProviderChain }
func (t *fakeTracker) AppendToChains(C.ProxyAdapter) {}
func (t *fakeTracker) RemoteDestination() string     { return "" }

type fakeRouteMonitor struct {
	defaultIface *control.Interface
	routes       map[netip.Addr]*control.Interface
}

func (m *fakeRouteMonitor) Start() error                         { return nil }
func (m *fakeRouteMonitor) Close() error                         { return nil }
func (m *fakeRouteMonitor) DefaultInterface() *control.Interface { return m.defaultIface }
func (m *fakeRouteMonitor) OverrideAndroidVPN() bool             { return false }
func (m *fakeRouteMonitor) AndroidVPNEnabled() bool              { return false }
func (m *fakeRouteMonitor) RegisterCallback(tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	return nil
}
func (m *fakeRouteMonitor) UnregisterCallback(*list.Element[tun.DefaultInterfaceUpdateCallback]) {}
func (m *fakeRouteMonitor) RouteInterface(destination netip.Addr, excludeInterface string) *control.Interface {
	if !destination.IsValid() || destination.IsUnspecified() {
		return nil
	}
	if ifObj, ok := m.routes[destination.Unmap()]; ok {
		if excludeInterface != "" && ifObj.Name == excludeInterface {
			return nil
		}
		return ifObj
	}
	return nil
}

func TestResetStaleState(t *testing.T) {
	pinnedInner := &fakeAdapter{fakePlainAdapter: fakePlainAdapter{name: "pinned-direct", iface: "Ethernet 2"}}
	defaultInner := &fakeAdapter{fakePlainAdapter: fakePlainAdapter{name: "default-proxy", iface: ""}}
	plainInner := &fakePlainAdapter{name: "plain-direct", iface: ""}

	proxies := map[string]C.Proxy{
		"pinned-direct": &fakeProxy{adapter: &fakeUpstreamWrapper{ProxyAdapter: pinnedInner}},
		"default-proxy": &fakeProxy{adapter: &fakeUpstreamWrapper{ProxyAdapter: defaultInner}},
		"plain-direct":  &fakeProxy{adapter: &fakeUpstreamWrapper{ProxyAdapter: plainInner}},
	}

	mgr := &statistic.Manager{}
	pinnedTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:  uuid.Must(uuid.NewV4()),
			Start: time.Now(),
			Chain: C.Chain{"pinned-direct", "group-pinned"},
		},
	}
	staleTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:  uuid.Must(uuid.NewV4()),
			Start: time.Now(),
			Chain: C.Chain{"default-proxy", "group-default"},
		},
	}
	mgr.Join(pinnedTracker)
	mgr.Join(staleTracker)

	closedConns, resetPools := resetStaleState(proxies, map[string]P.ProxyProvider{}, mgr, "Wi-Fi")

	if closedConns != 1 || resetPools != 1 {
		t.Fatalf("got closedConns=%d resetPools=%d, want 1 and 1", closedConns, resetPools)
	}
	if !staleTracker.closed {
		t.Fatal("expected unpinned default-proxy connection to be closed")
	}
	if pinnedTracker.closed {
		t.Fatal("expected pinned-direct connection pinned to Ethernet 2 to remain open")
	}
	if defaultInner.resetHits != 1 {
		t.Fatalf("expected wrapped default-proxy ResetSessions to be called once, got %d", defaultInner.resetHits)
	}
	if pinnedInner.resetHits != 0 {
		t.Fatalf("expected pinned-direct ResetSessions not to be called, got %d", pinnedInner.resetHits)
	}
}

func TestResetStaleStateFailbackPreservesHealthyPinnedInterface(t *testing.T) {
	pinnedDirect := &fakeAdapter{fakePlainAdapter: fakePlainAdapter{name: "pinned-direct", iface: "Ethernet 2"}}
	defaultProxy := &fakeAdapter{fakePlainAdapter: fakePlainAdapter{name: "default-proxy", iface: ""}}

	proxies := map[string]C.Proxy{
		"pinned-direct": &fakeProxy{adapter: &fakeUpstreamWrapper{ProxyAdapter: pinnedDirect}},
		"default-proxy": &fakeProxy{adapter: &fakeUpstreamWrapper{ProxyAdapter: defaultProxy}},
	}

	mgr := &statistic.Manager{}
	nicTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:  uuid.Must(uuid.NewV4()),
			Start: time.Now(),
			Chain: C.Chain{"pinned-direct", "group-pinned"},
		},
	}
	proxyTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:  uuid.Must(uuid.NewV4()),
			Start: time.Now(),
			Chain: C.Chain{"default-proxy", "proxy-group"},
		},
	}
	mgr.Join(nicTracker)
	mgr.Join(proxyTracker)

	closedConns, resetPools := resetStaleStateWithOpts(
		proxies,
		map[string]P.ProxyProvider{},
		mgr,
		"Ethernet 2",
		func(name string) bool { return name == "Ethernet 2" },
		nil,
	)

	if closedConns != 1 || resetPools != 1 {
		t.Fatalf("got closedConns=%d resetPools=%d, want 1 and 1", closedConns, resetPools)
	}
	if nicTracker.closed {
		t.Fatal("expected pinned-direct on healthy Ethernet 2 to remain open on Wi-Fi failback")
	}
	if pinnedDirect.resetHits != 0 {
		t.Fatalf("expected pinned-direct ResetSessions not to be called when Ethernet 2 is healthy, got %d", pinnedDirect.resetHits)
	}
	if !proxyTracker.closed || defaultProxy.resetHits != 1 {
		t.Fatal("expected unpinned default-proxy connection and pool to be reset")
	}
}

func TestResetStaleStateDownPinnedInterfaceIsClosed(t *testing.T) {
	pinnedDirect := &fakeAdapter{fakePlainAdapter: fakePlainAdapter{name: "pinned-direct", iface: "Ethernet 2"}}
	proxies := map[string]C.Proxy{
		"pinned-direct": &fakeProxy{adapter: &fakeUpstreamWrapper{ProxyAdapter: pinnedDirect}},
	}

	mgr := &statistic.Manager{}
	nicTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:  uuid.Must(uuid.NewV4()),
			Start: time.Now(),
			Chain: C.Chain{"pinned-direct", "group-pinned"},
		},
	}
	mgr.Join(nicTracker)

	closedConns, resetPools := resetStaleStateWithOpts(
		proxies,
		map[string]P.ProxyProvider{},
		mgr,
		"Ethernet 2",
		func(string) bool { return false },
		nil,
	)

	if closedConns != 1 || resetPools != 1 {
		t.Fatalf("got closedConns=%d resetPools=%d, want 1 and 1", closedConns, resetPools)
	}
	if !nicTracker.closed {
		t.Fatal("expected pinned-direct to be closed when Ethernet 2 is down")
	}
	if pinnedDirect.resetHits != 1 {
		t.Fatalf("expected pinned-direct ResetSessions to be called once, got %d", pinnedDirect.resetHits)
	}
}

func TestResetStaleStatePreservesDirectOnHealthyRoutedInterface(t *testing.T) {
	mgr := &statistic.Manager{}
	vpnTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:     uuid.Must(uuid.NewV4()),
			Start:    time.Now(),
			Chain:    C.Chain{"DIRECT"},
			Metadata: &C.Metadata{DstIP: netip.MustParseAddr("10.10.0.5")},
		},
	}
	nicDirectTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:     uuid.Must(uuid.NewV4()),
			Start:    time.Now(),
			Chain:    C.Chain{"DIRECT"},
			Metadata: &C.Metadata{DstIP: netip.MustParseAddr("10.20.0.1")},
		},
	}
	inetTracker := &fakeTracker{
		info: &statistic.TrackerInfo{
			UUID:     uuid.Must(uuid.NewV4()),
			Start:    time.Now(),
			Chain:    C.Chain{"DIRECT"},
			Metadata: &C.Metadata{DstIP: netip.MustParseAddr("198.51.100.1")},
		},
	}
	mgr.Join(vpnTracker)
	mgr.Join(nicDirectTracker)
	mgr.Join(inetTracker)

	routeLookup := func(dst netip.Addr) string {
		switch dst.String() {
		case "10.10.0.5":
			return "VPN 0"
		case "10.20.0.1":
			return "Ethernet 2"
		default:
			return ""
		}
	}

	closedConns, resetPools := resetStaleStateWithOpts(
		map[string]C.Proxy{},
		map[string]P.ProxyProvider{},
		mgr,
		"Ethernet 2",
		func(name string) bool { return name == "Ethernet 2" || name == "VPN 0" },
		routeLookup,
	)

	if closedConns != 1 || resetPools != 0 {
		t.Fatalf("got closedConns=%d resetPools=%d, want 1 and 0", closedConns, resetPools)
	}
	if vpnTracker.closed {
		t.Fatal("expected VPN routed DIRECT connection to stay open")
	}
	if nicDirectTracker.closed {
		t.Fatal("expected healthy Ethernet 2 routed DIRECT connection to stay open")
	}
	if !inetTracker.closed {
		t.Fatal("expected default-routed DIRECT connection to be closed on interface switch")
	}
}

func TestDialerInterfaceFinderRouteAwareness(t *testing.T) {
	for _, tunName := range []string{"test-tun", "custom-utun9"} {
		t.Run(tunName, func(t *testing.T) {
			mon := &fakeRouteMonitor{
				defaultIface: &control.Interface{Index: 8, Name: "Wi-Fi"},
				routes: map[netip.Addr]*control.Interface{
					netip.MustParseAddr("10.10.0.5"):  {Index: 4, Name: "VPN 0"},
					netip.MustParseAddr("198.18.0.5"): {Index: 2, Name: tunName},
				},
			}
			finder := &cDialerInterfaceFinder{
				tunName:                 tunName,
				defaultInterfaceMonitor: mon,
			}

			if got := finder.DefaultInterfaceName(netip.MustParseAddr("10.10.0.5")); got != "VPN 0" {
				t.Fatalf("got %q for 10.10.0.5, want VPN 0", got)
			}
			if got := finder.DefaultInterfaceName(netip.MustParseAddr("203.0.113.1")); got != "Wi-Fi" {
				t.Fatalf("got %q for 203.0.113.1, want Wi-Fi", got)
			}
			if got := finder.DefaultInterfaceName(netip.MustParseAddr("198.18.0.5")); got != "Wi-Fi" {
				t.Fatalf("got %q for 198.18.0.5 (TUN interface %q must be ignored), want Wi-Fi", got, tunName)
			}
			if got := finder.DefaultInterfaceName(netip.IPv4Unspecified()); got != "Wi-Fi" {
				t.Fatalf("got %q for 0.0.0.0, want Wi-Fi", got)
			}
		})
	}
}

func TestInterfaceSwitchTracker(t *testing.T) {
	var tracker interfaceSwitchTracker

	if prev, changed := tracker.record("Wi-Fi"); changed || prev != "" {
		t.Fatalf("initial interface record should not report changed, got (%q, %v)", prev, changed)
	}
	if _, changed := tracker.record("Wi-Fi"); changed {
		t.Fatal("repeated same interface should not report changed")
	}
	if prev, changed := tracker.record("Ethernet 2"); !changed || prev != "Wi-Fi" {
		t.Fatalf("expected switch from Wi-Fi, got (%q, %v)", prev, changed)
	}
	if prev, changed := tracker.record(""); !changed || prev != "Ethernet 2" {
		t.Fatalf("expected switch to empty from Ethernet 2, got (%q, %v)", prev, changed)
	}
}
