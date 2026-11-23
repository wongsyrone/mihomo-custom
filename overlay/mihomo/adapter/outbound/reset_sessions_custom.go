// Problem:
// Closing tracked upper-level streams in statistic.DefaultManager does not tear
// down internal HTTP/2, HTTP/3, QUIC, smux, SSH, keep-alive pools, or persistent
// L3 sockets maintained by stateful outbound adapters. Calling Close() directly
// permanently disables those clients.
//
// Solution:
// Define SessionResetter, expose Upstream()/ResetSessions() on
// autoCloseProxyAdapter so wrapper layers delegate cleanly, and implement
// non-destructive ResetSessions() across all stateful outbound adapters
// (excluding autonomous mesh daemons Tailscale, EasyTier, and ZeroTier whose
// own roaming engines manage control planes and peer paths).

package outbound

import (
	"context"
	"errors"
	"time"

	"github.com/metacubex/mihomo/common/httputils"
)

// SessionResetter clears stale pooled/persistent transport sessions without permanently closing the adapter.
type SessionResetter interface {
	ResetSessions()
}

func (p *autoCloseProxyAdapter) ResetSessions() {
	if p == nil {
		return
	}
	if r, ok := p.ProxyAdapter.(SessionResetter); ok {
		r.ResetSessions()
	}
}

func (p *autoCloseProxyAdapter) Upstream() any {
	if p == nil {
		return nil
	}
	return p.ProxyAdapter
}

// ResetSessions clears AnyTLS pooled sessions bound to the old network interface.
func (t *AnyTLS) ResetSessions() {
	if t != nil && t.client != nil {
		t.client.ResetSessions()
	}
}

// ResetSessions clears active mux sessions without shutting down SingMux and cascades to the inner adapter.
func (s *SingMux) ResetSessions() {
	if s == nil {
		return
	}
	if s.client != nil {
		s.client.Reset()
	}
	if r, ok := s.ProxyAdapter.(SessionResetter); ok {
		r.ResetSessions()
	}
}

// ResetSessions resets pooled gRPC/gun transports so subsequent dials use the new default interface.
func (t *Trojan) ResetSessions() {
	if t != nil && t.gunClient != nil {
		t.gunClient.ResetSessions()
	}
}

// ResetSessions resets pooled gRPC/gun and xhttp transports so subsequent dials use the new default interface.
func (v *Vless) ResetSessions() {
	if v == nil {
		return
	}
	if v.gunClient != nil {
		v.gunClient.ResetSessions()
	}
	if v.xhttpClient != nil {
		v.xhttpClient.ResetSessions()
	}
}

// ResetSessions resets pooled gRPC/gun and mekya transports so subsequent dials use the new default interface.
func (v *Vmess) ResetSessions() {
	if v == nil {
		return
	}
	if v.gunClient != nil {
		v.gunClient.ResetSessions()
	}
	if v.mekyaClient != nil {
		v.mekyaClient.ResetSessions()
	}
}

// ResetSessions closes the active Hysteria2 QUIC connection and Realm HTTP transport without permanently disabling Hysteria2.
func (h *Hysteria2) ResetSessions() {
	if h == nil {
		return
	}
	if h.client != nil {
		_ = h.client.CloseWithError(errors.New("default interface changed"))
	}
	if h.realmHTTPTransport != nil {
		httputils.CloseTransport(h.realmHTTPTransport)
	}
}

// ResetSessions drops the cached Hysteria QUIC connection while keeping the client reusable.
func (h *Hysteria) ResetSessions() {
	if h != nil && h.client != nil {
		h.client.ResetSessions()
	}
}

// ResetSessions forcefully closes pooled TUIC QUIC clients so subsequent dials open fresh connections.
func (t *Tuic) ResetSessions() {
	if t != nil && t.client != nil {
		t.client.ResetSessions()
	}
}

// ResetSessions drops the cached ShadowQuic QUIC connection while keeping future dials functional.
func (s *ShadowQuic) ResetSessions() {
	if s != nil && s.client != nil {
		s.client.ResetSessions()
	}
}

// ResetSessions clears pooled TrustTunnel clients so future dials open fresh H2/H3 connections.
func (t *TrustTunnel) ResetSessions() {
	if t != nil && t.client != nil {
		t.client.ResetSessions()
	}
}

// ResetSessions detaches and closes the shared SSH client so the next DialContext establishes a new connection.
func (s *Ssh) ResetSessions() {
	if s == nil {
		return
	}
	s.cMutex.Lock()
	client := s.client
	s.client = nil
	s.cMutex.Unlock()

	if client != nil {
		_ = client.Close()
	}
}

// ResetSessions evicts idle pooled Snell connections and invalidates outstanding reuse leases.
func (s *Snell) ResetSessions() {
	if s != nil && s.pool != nil {
		s.pool.Reset()
	}
}

// ResetSessions closes active kcptun mux sessions while leaving Shadowsocks ready for new streams.
func (ss *ShadowSocks) ResetSessions() {
	if ss != nil && ss.kcptunClient != nil {
		ss.kcptunClient.ResetSessions()
	}
}

// ResetSessions clears cached Sudoku multiplex sessions and HTTP mask transports while keeping future dials working.
func (s *Sudoku) ResetSessions() {
	if s == nil {
		return
	}
	if s.muxDialer != nil {
		s.muxDialer.ResetSessions()
	}
	s.resetHTTPMaskClient()
}

// ResetSessions stops the Mieru mux client so the next dial lazily restarts it via ensureClientIsRunning.
func (m *Mieru) ResetSessions() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && m.client.IsRunning() {
		_ = m.client.Stop()
	}
}

func (w *Masque) registerTunnelCancel(cancel context.CancelFunc) uint64 {
	w.tunnelMu.Lock()
	defer w.tunnelMu.Unlock()
	w.tunnelGen++
	w.tunnelCancel = cancel
	w.running.Store(true)
	return w.tunnelGen
}

func (w *Masque) clearTunnelIfCurrent(gen uint64) {
	w.tunnelMu.Lock()
	defer w.tunnelMu.Unlock()
	if w.tunnelGen == gen {
		w.tunnelCancel = nil
		w.running.Store(false)
	}
}

// ResetSessions tears down the current MASQUE transport tunnel without closing the ipStack or canceling runCtx.
func (w *Masque) ResetSessions() {
	if w == nil {
		return
	}
	if w.l4Client != nil {
		w.l4Client.ResetSessions()
		return
	}

	w.tunnelMu.Lock()
	cancel := w.tunnelCancel
	w.tunnelCancel = nil
	w.tunnelGen++
	w.running.Store(false)
	w.tunnelMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if w.h2Transport != nil {
		httputils.CloseTransport(w.h2Transport)
	}
}

// ResetSessions tears down the active OpenVPN tunnel without canceling runCtx so the next dial reconnects cleanly.
func (o *OpenVPN) ResetSessions() {
	if o == nil || o.runLock == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := o.runLock.Acquire(ctx, 1)
	cancel()
	if err != nil {
		return
	}
	client := o.client
	tunDevice := o.tunDevice
	o.client = nil
	o.tunDevice = nil
	o.running = false
	o.runLock.Release(1)

	if client != nil {
		_ = client.Close()
	}
	if tunDevice != nil {
		_ = tunDevice.Close()
	}
}

// ResetSessions rebinds the active WireGuard/AmneziaWG UDP socket onto the current network interface.
func (w *WireGuard) ResetSessions() {
	if w == nil {
		return
	}
	w.deviceMutex.Lock()
	wgDevice := w.device
	w.deviceMutex.Unlock()
	if wgDevice != nil {
		_ = wgDevice.BindUpdate()
	}
}
