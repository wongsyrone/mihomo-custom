package outbound

import (
	"testing"

	C "github.com/metacubex/mihomo/constant"
)

var (
	_ SessionResetter = (*AnyTLS)(nil)
	_ SessionResetter = (*SingMux)(nil)
	_ SessionResetter = (*Trojan)(nil)
	_ SessionResetter = (*Vless)(nil)
	_ SessionResetter = (*Vmess)(nil)
	_ SessionResetter = (*Hysteria2)(nil)
	_ SessionResetter = (*Hysteria)(nil)
	_ SessionResetter = (*Tuic)(nil)
	_ SessionResetter = (*ShadowQuic)(nil)
	_ SessionResetter = (*TrustTunnel)(nil)
	_ SessionResetter = (*Ssh)(nil)
	_ SessionResetter = (*Snell)(nil)
	_ SessionResetter = (*ShadowSocks)(nil)
	_ SessionResetter = (*Sudoku)(nil)
	_ SessionResetter = (*Mieru)(nil)
	_ SessionResetter = (*Masque)(nil)
	_ SessionResetter = (*OpenVPN)(nil)
	_ SessionResetter = (*WireGuard)(nil)
	_ SessionResetter = (*autoCloseProxyAdapter)(nil)
)

type fakeResetInnerAdapter struct {
	*Base
	resetCount int
}

func (f *fakeResetInnerAdapter) ResetSessions() {
	f.resetCount++
}

type fakeWireGuardDevice struct {
	bindUpdates int
	closed      bool
}

func (d *fakeWireGuardDevice) Close()              { d.closed = true }
func (d *fakeWireGuardDevice) IpcSet(string) error { return nil }
func (d *fakeWireGuardDevice) BindUpdate() error   { d.bindUpdates++; return nil }

func TestAutoCloseAndSingMuxResetSessions(t *testing.T) {
	inner := &fakeResetInnerAdapter{
		Base: NewBase(BaseOption{Name: "test-proxy", Addr: "example.com:443", Type: C.Trojan}),
	}
	muxAdapter, err := NewSingMux(SingMuxOption{Enabled: true, Protocol: "h2mux"}, inner)
	if err != nil {
		t.Fatalf("NewSingMux failed: %v", err)
	}
	wrapped := NewAutoCloseProxyAdapter(muxAdapter).(*autoCloseProxyAdapter)
	if wrapped.Upstream() != muxAdapter {
		t.Fatal("expected autoCloseProxyAdapter.Upstream to expose SingMux")
	}

	wrapped.ResetSessions()
	if inner.resetCount != 1 {
		t.Fatalf("expected ResetSessions to cascade to inner adapter once, got %d", inner.resetCount)
	}
	_ = wrapped.Close()
}

func TestWireGuardResetSessionsCallsBindUpdate(t *testing.T) {
	dev := &fakeWireGuardDevice{}
	wg := &WireGuard{device: dev}
	wg.ResetSessions()
	if dev.bindUpdates != 1 {
		t.Fatalf("expected BindUpdate to be called once, got %d", dev.bindUpdates)
	}
	if dev.closed {
		t.Fatal("expected WireGuard.ResetSessions not to close the device")
	}
}

func TestOutboundResetSessionsNilSafe(t *testing.T) {
	(&AnyTLS{}).ResetSessions()
	(&SingMux{}).ResetSessions()
	(&Trojan{}).ResetSessions()
	(&Vless{}).ResetSessions()
	(&Vmess{}).ResetSessions()
	(&Hysteria2{}).ResetSessions()
	(&Hysteria{}).ResetSessions()
	(&Tuic{}).ResetSessions()
	(&ShadowQuic{}).ResetSessions()
	(&TrustTunnel{}).ResetSessions()
	(&Ssh{}).ResetSessions()
	(&Snell{}).ResetSessions()
	(&ShadowSocks{}).ResetSessions()
	(&Sudoku{}).ResetSessions()
	(&Mieru{}).ResetSessions()
	(&Masque{}).ResetSessions()
	(&OpenVPN{}).ResetSessions()
	(&WireGuard{}).ResetSessions()
}
