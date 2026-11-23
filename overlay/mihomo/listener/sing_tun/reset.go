// Problem:
// When sing-tun's DefaultInterfaceMonitor switches to another default interface
// after the active adapter disconnects, upstream server.go only flushes the
// interface cache and resets DNS resolver connections. Existing TCP/UDP flows in
// statistic.DefaultManager and pooled outbound client sessions remain bound to
// the vanished source IP until long timeouts expire, stalling failover. In
// addition, parsed proxy adapters are wrapped in autoCloseProxyAdapter, which
// hides extra methods from type assertions unless unwrapped.
//
// Solution:
// Track actual default-interface transitions via interfaceSwitchTracker and
// reset stale state in two phases:
// 1. Unwrap wrapped adapters and run ResetSessions() concurrently bounded by
//    500ms first so session pools mark closed and close hooks do not re-queue
//    dead sessions.
// 2. Close active tracked connections in statistic.DefaultManager while
//    preserving proxies and connections explicitly pinned via interface-name
//    to a different healthy interface.

package sing_tun

import (
	"sync"
	"time"

	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel/statistic"
	"github.com/metacubex/sing/common/control"
)

const resetSessionTimeout = 500 * time.Millisecond

type sessionResetter interface {
	ResetSessions()
}

type interfaceSwitchTracker struct {
	mu      sync.Mutex
	hasLast bool
	last    string
}

func (t *interfaceSwitchTracker) record(newName string) (prev string, changed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	prevHas := t.hasLast
	prev = t.last
	t.hasLast = true
	t.last = newName
	return prev, prevHas && prev != newName
}

func (t *interfaceSwitchTracker) onInterfaceUpdate(defaultInterface *control.Interface, tunnel C.Tunnel) {
	newName := ""
	if defaultInterface != nil {
		newName = defaultInterface.Name
	}
	if prev, changed := t.record(newName); changed {
		resetStaleConnectionsOnInterfaceChange(tunnel, prev)
	}
}

func unwrapAdapter(a any) any {
	for i := 0; i < 8; i++ {
		if a == nil {
			return nil
		}
		u, ok := a.(interface{ Upstream() any })
		if !ok {
			return a
		}
		next := u.Upstream()
		if next == nil {
			return a
		}
		a = next
	}
	return a
}

func resetStaleConnectionsOnInterfaceChange(tunnel C.Tunnel, oldInterface string) {
	var proxies map[string]C.Proxy
	if pl, ok := tunnel.(interface{ Proxies() map[string]C.Proxy }); ok {
		proxies = pl.Proxies()
	}
	var providers map[string]P.ProxyProvider
	if rp, ok := tunnel.(P.Tunnel); ok {
		providers = rp.Providers()
	}
	closedConns, resetPools := resetStaleState(proxies, providers, statistic.DefaultManager, oldInterface)
	if closedConns > 0 || resetPools > 0 {
		log.Warnln("[TUN] reset stale state after interface switch (from %q): closed %d connections, reset %d session pools", oldInterface, closedConns, resetPools)
	}
}

func resetStaleState(proxies map[string]C.Proxy, providers map[string]P.ProxyProvider, manager *statistic.Manager, oldInterface string) (closedConns int, resetPools int) {
	allProxies := make([]C.Proxy, 0, len(proxies))
	seen := make(map[string]struct{}, len(proxies))
	addProxy := func(p C.Proxy) {
		if p == nil {
			return
		}
		name := p.Name()
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		allProxies = append(allProxies, p)
	}

	for _, p := range proxies {
		addProxy(p)
	}
	for _, pd := range providers {
		if pd == nil {
			continue
		}
		for _, p := range pd.Proxies() {
			addProxy(p)
		}
	}

	pinnedOther := make(map[string]bool)
	for _, p := range allProxies {
		ifName := p.ProxyInfo().Interface
		if ifName != "" && ifName != oldInterface {
			pinnedOther[p.Name()] = true
		}
	}

	// Reset pooled sessions first so stream close hooks observe closed state
	// and do not re-queue dead sessions back into idle pools.
	resetters := make([]sessionResetter, 0)
	for _, p := range allProxies {
		if pinnedOther[p.Name()] {
			continue
		}
		target := unwrapAdapter(p.Adapter())
		if target == nil {
			target = p
		}
		if r, ok := target.(sessionResetter); ok {
			resetters = append(resetters, r)
		} else if r, ok := any(p).(sessionResetter); ok {
			resetters = append(resetters, r)
		}
	}

	resetPools = len(resetters)
	if resetPools > 0 {
		var wg sync.WaitGroup
		wg.Add(resetPools)
		for _, r := range resetters {
			go func(resetter sessionResetter) {
				defer wg.Done()
				resetter.ResetSessions()
			}(r)
		}
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(resetSessionTimeout):
		}
	}

	if manager != nil {
		manager.Range(func(c statistic.Tracker) bool {
			info := c.Info()
			if info != nil && len(info.Chain) > 0 && pinnedOther[info.Chain[0]] {
				return true
			}
			_ = c.Close()
			closedConns++
			return true
		})
	}

	return closedConns, resetPools
}
