package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixtureFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertValidGoFiles(t *testing.T, root string, rels []string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(rels))
	fset := token.NewFileSet()
	for _, rel := range rels {
		p := filepath.Join(root, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(b)
		if strings.Contains(text, "\r") {
			t.Fatalf("%s contains CR characters", rel)
		}
		if _, err := parser.ParseFile(fset, p, b, parser.AllErrors); err != nil {
			t.Fatalf("re-parse %s failed: %v\n%s", rel, err, text)
		}
		out[rel] = text
	}
	return out
}

func TestPatchSingTun(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"monitor.go": `package tun

import "github.com/metacubex/sing/common/control"

type DefaultInterfaceMonitorOptions struct {
	InterfaceFinder       control.InterfaceFinder
	OverrideAndroidVPN    bool
	UnderNetworkExtension bool
	ExtraUpstreamField    string
}
`,
		"monitor_shared.go": `package tun

import (
	"sync"
	"sync/atomic"
)

type defaultInterfaceMonitor struct {
	interfaceFinder       any
	overrideAndroidVPN    bool
	underNetworkExtension bool
	defaultInterface      atomic.Pointer[int]
	networkMonitor        any
	logger                any
	access                sync.Mutex
}

func NewDefaultInterfaceMonitor(networkMonitor any, logger any, options DefaultInterfaceMonitorOptions) (any, error) {
	return &defaultInterfaceMonitor{
		interfaceFinder:       options.InterfaceFinder,
		overrideAndroidVPN:    options.OverrideAndroidVPN,
		underNetworkExtension: options.UnderNetworkExtension,
		networkMonitor:        networkMonitor,
		logger:                logger,
	}, nil
}
`,
		"monitor_windows.go": `package tun

func (m *defaultInterfaceMonitor) checkUpdate() error {
	rows, err := getRows()
	if err != nil {
		return err
	}
	// upstream comment added near metric init
	lowestMetric := ^uint32(0)
	alias := ""
	var index int

	for _, row := range rows {
		if row.Skip() {
			continue
		}
		metric := row.Metric + 1
		if metric < lowestMetric {
			lowestMetric = metric
			alias = ifrow.Alias()
			index = int(ifrow.InterfaceIndex)
		}
	}
	if alias == "" {
		return ErrNoRoute
	}
	return nil
}
`,
	})

	if err := patchSingTun(dir); err != nil {
		t.Fatalf("patchSingTun failed: %v", err)
	}

	got := assertValidGoFiles(t, dir, []string{"monitor.go", "monitor_shared.go", "monitor_windows.go"})
	if !strings.Contains(got["monitor.go"], "PreferredInterfaces []string") {
		t.Fatalf("monitor.go missing PreferredInterfaces:\n%s", got["monitor.go"])
	}
	if !strings.Contains(got["monitor_shared.go"], "filterPreferredInterfaces(options.PreferredInterfaces)") {
		t.Fatalf("monitor_shared.go missing preferredInterfaces wiring:\n%s", got["monitor_shared.go"])
	}
	if strings.Contains(got["monitor_windows.go"], "lowestMetric") {
		t.Fatalf("monitor_windows.go still contains lowestMetric:\n%s", got["monitor_windows.go"])
	}
	if !strings.Contains(got["monitor_windows.go"], "_routeSelector.add(ifrow.Alias(), int(ifrow.InterfaceIndex), metric)") ||
		!strings.Contains(got["monitor_windows.go"], "_routeSelector.selectFor(m)") {
		t.Fatalf("monitor_windows.go missing selector calls:\n%s", got["monitor_windows.go"])
	}
}

func TestPatchMihomoTunAndOutbound(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"config/config.go": `package config

import LC "github.com/metacubex/mihomo/listener/config"

type RawTun struct {
	Enable              bool     ` + "`yaml:\"enable\"`" + `
	AutoDetectInterface bool     ` + "`yaml:\"auto-detect-interface\"`" + `
	UpstreamExtra       bool     ` + "`yaml:\"upstream-extra\"`" + `
}

func parseTun(rawTun RawTun, general *General) error {
	general.Tun = LC.Tun{
		Enable:              rawTun.Enable,
		AutoDetectInterface: rawTun.AutoDetectInterface,
	}
	return nil
}
`,
		"listener/config/tun.go": `package config

import "golang.org/x/exp/slices"

type Tun struct {
	Enable              bool
	AutoDetectInterface bool
}

func (t *Tun) Equal(other Tun) bool {
	if t.Enable != other.Enable {
		return false
	}
	if t.AutoDetectInterface != other.AutoDetectInterface {
		return false
	}
	return true
}
`,
		"listener/sing_tun/server.go": `package sing_tun

func New(options LC.Tun, tunnel C.Tunnel) error {
	if options.AutoRoute || options.AutoDetectInterface {
		defaultInterfaceMonitor, err = tun.NewDefaultInterfaceMonitor(networkUpdateMonitor, log.SingLogger, tun.DefaultInterfaceMonitorOptions{InterfaceFinder: interfaceFinder, OverrideAndroidVPN: overrideAndroidVPN})
		if err != nil {
			return err
		}
		defaultInterfaceMonitor.RegisterCallback(func(defaultInterface *control.Interface, event int) {
			iface.FlushCache()
			resolver.ResetConnection()
		})
	}
	return nil
}
`,
		"adapter/outbound/wireguard.go": `package outbound

type wireguardGoDevice interface {
	Close()
	IpcSet(uapiConf string) error
}
`,
		"transport/tuic/types/type.go": `package types

type Client interface {
	OpenStreams() int64
	Close()
}
`,
		"transport/tuic/v4/client.go": `package v4

import "context"

func (t *clientImpl) getQuicConn(ctx context.Context) (*quic.Conn, error) {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()
	if t.quicConn != nil {
		return t.quicConn, nil
	}
	quicConn, err := t.dialFn(ctx)
	if err != nil {
		return nil, err
	}
	t.quicConn = quicConn
	return quicConn, nil
}
`,
		"transport/tuic/v5/client.go": `package v5

import "context"

func (t *clientImpl) getQuicConn(ctx context.Context) (*quic.Conn, error) {
	t.connMutex.Lock()
	defer t.connMutex.Unlock()
	if t.quicConn != nil {
		return t.quicConn, nil
	}
	quicConn, err := t.dialFn(ctx)
	if err != nil {
		return nil, err
	}
	t.quicConn = quicConn
	return quicConn, nil
}
`,
		"transport/snell/pool.go": `package snell

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
)

type Pool struct {
	pool *pool.Pool[*Snell]
}

func (p *Pool) GetContext(ctx context.Context) (net.Conn, error) {
	elm, err := p.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	return &PoolConn{Snell: elm, pool: p}, nil
}

type PoolConn struct {
	*Snell
	pool       *Pool
	closeOnce  sync.Once
	peerClosed atomic.Bool
}

func (pc *PoolConn) Close() error {
	pc.closeOnce.Do(func() {
		if !pc.peerClosed.Load() {
			_ = pc.Snell.Close()
			return
		}
		pc.pool.put(pc.Snell)
	})
	return nil
}
`,
		"adapter/outbound/hysteria2.go": `package outbound

import "github.com/metacubex/http"

type Hysteria2 struct {
	*Base
	option *Hysteria2Option
	client *hysteria2.Client
}

func NewHysteria2(option Hysteria2Option) (*Hysteria2, error) {
	outbound := &Hysteria2{}
	var clientOptions hysteria2.ClientOptions
	if option.RealmOpts.Enable {
		clientOptions.RealmOptions = &realm.Options{
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		}
	}
	return outbound, nil
}
`,
		"adapter/outbound/masque.go": `package outbound

import (
	"context"

	"github.com/metacubex/mihomo/common/atomic"
)

type Masque struct {
	*Base
	runCtx  context.Context
	running atomic.Bool
}

func (w *Masque) startLocked(ctx context.Context) error {
	w.running.Store(true)

	runCtx, runCancel := context.WithCancel(w.runCtx)
	contextutils.AfterFunc(runCtx, func() {
		w.running.Store(false)
		_ = ipConn.Close()
	})
	return nil
}
`,
	})

	if err := patchMihomoTun(dir); err != nil {
		t.Fatalf("patchMihomoTun failed: %v", err)
	}
	if err := patchMihomoOutbound(dir); err != nil {
		t.Fatalf("patchMihomoOutbound failed: %v", err)
	}

	got := assertValidGoFiles(t, dir, []string{
		"config/config.go",
		"listener/config/tun.go",
		"listener/sing_tun/server.go",
		"adapter/outbound/wireguard.go",
		"transport/tuic/types/type.go",
		"transport/tuic/v4/client.go",
		"transport/tuic/v5/client.go",
		"transport/snell/pool.go",
		"adapter/outbound/hysteria2.go",
		"adapter/outbound/masque.go",
	})

	if !strings.Contains(got["config/config.go"], "default-interface-priority,omitempty") {
		t.Fatalf("config/config.go missing omitempty tag:\n%s", got["config/config.go"])
	}
	if !strings.Contains(got["listener/config/tun.go"], "slices.Equal(t.DefaultInterfacePriority, other.DefaultInterfacePriority)") {
		t.Fatalf("listener/config/tun.go missing Equal check:\n%s", got["listener/config/tun.go"])
	}
	if !strings.Contains(got["listener/sing_tun/server.go"], "_ifSwitchTracker.onInterfaceUpdate(defaultInterface, tunnel)") {
		t.Fatalf("server.go missing interface switch hook:\n%s", got["listener/sing_tun/server.go"])
	}
	if !strings.Contains(got["adapter/outbound/wireguard.go"], "BindUpdate() error") {
		t.Fatalf("wireguard.go missing BindUpdate:\n%s", got["adapter/outbound/wireguard.go"])
	}
	if !strings.Contains(got["transport/tuic/v4/client.go"], "types.ClientClosed") {
		t.Fatalf("tuic v4 missing closed guard:\n%s", got["transport/tuic/v4/client.go"])
	}
	if !strings.Contains(got["transport/snell/pool.go"], "pc.pool.gen.Load()") {
		t.Fatalf("snell/pool.go missing gen check:\n%s", got["transport/snell/pool.go"])
	}
	if !strings.Contains(got["adapter/outbound/hysteria2.go"], "outbound.realmHTTPTransport = tr") {
		t.Fatalf("hysteria2.go missing realmHTTPTransport capture:\n%s", got["adapter/outbound/hysteria2.go"])
	}
	if !strings.Contains(got["adapter/outbound/masque.go"], "\"sync\"") ||
		!strings.Contains(got["adapter/outbound/masque.go"], "registerTunnelCancel(runCancel)") ||
		!strings.Contains(got["adapter/outbound/masque.go"], "clearTunnelIfCurrent(tunnelGen)") {
		t.Fatalf("masque.go missing tunnel lifecycle hooks:\n%s", got["adapter/outbound/masque.go"])
	}
}

func TestPatchFailsExplicitlyOnMissingAnchor(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFiles(t, dir, map[string]string{
		"monitor.go": "package tun\ntype RenamedOptions struct{}\n",
	})
	if err := patchSingTunMonitorGo(filepath.Join(dir, "monitor.go")); err == nil {
		t.Fatal("expected error when DefaultInterfaceMonitorOptions is missing")
	}
}

func TestCopyOverlayDirNormalizesLF(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFixtureFiles(t, src, map[string]string{
		"sub/example.go": "package sub\r\n\r\nfunc ok() {}\r\n",
	})
	n, err := copyOverlayDir(src, dst)
	if err != nil {
		t.Fatalf("copyOverlayDir failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 file copied, got %d", n)
	}
	got, err := os.ReadFile(filepath.Join(dst, "sub/example.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "\r") {
		t.Fatal("expected LF normalization in copied overlay")
	}
}
