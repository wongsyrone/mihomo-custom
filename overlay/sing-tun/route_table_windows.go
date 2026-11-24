//go:build windows

package tun

import (
	"github.com/metacubex/sing-tun/internal/winipcfg"
	"golang.org/x/sys/windows"
)

func (m *defaultInterfaceMonitor) refreshRouteTable() {
	rows, err := winipcfg.GetIPForwardTable2(windows.AF_UNSPEC)
	if err != nil {
		return
	}

	type ifRowResult struct {
		row *winipcfg.MibIfRow2
		ok  bool
	}
	type ipIfKey struct {
		luid   winipcfg.LUID
		family winipcfg.AddressFamily
	}
	type ipIfResult struct {
		row *winipcfg.MibIPInterfaceRow
		ok  bool
	}

	ifRows := make(map[winipcfg.LUID]ifRowResult)
	ipIfs := make(map[ipIfKey]ipIfResult)

	entries := make([]routeEntry, 0, len(rows))
	for _, row := range rows {
		if row.DestinationPrefix.PrefixLength == 0 {
			continue
		}
		if row.NextHop.Addr() == zeroTierFakeGatewayIp {
			continue
		}
		family := row.DestinationPrefix.RawPrefix.Family
		if family != winipcfg.AddressFamily(windows.AF_INET) && family != winipcfg.AddressFamily(windows.AF_INET6) {
			continue
		}
		prefix := row.DestinationPrefix.Prefix()
		if !isEligibleRoutePrefix(prefix) {
			continue
		}

		ifRes, cached := ifRows[row.InterfaceLUID]
		if !cached {
			r, rErr := row.InterfaceLUID.Interface()
			if rErr == nil && r != nil {
				ifRes = ifRowResult{row: r, ok: true}
			}
			ifRows[row.InterfaceLUID] = ifRes
		}
		// Keep IfTypePropVirtual adapters (e.g. VPN tunnels); TUN itself is excluded at lookup time.
		if !ifRes.ok || ifRes.row.OperStatus != winipcfg.IfOperStatusUp || ifRes.row.Type == winipcfg.IfTypeSoftwareLoopback {
			continue
		}

		ipKey := ipIfKey{luid: row.InterfaceLUID, family: family}
		ipRes, ipCached := ipIfs[ipKey]
		if !ipCached {
			r, rErr := row.InterfaceLUID.IPInterface(family)
			if rErr == nil && r != nil {
				ipRes = ipIfResult{row: r, ok: true}
			}
			ipIfs[ipKey] = ipRes
		}
		if !ipRes.ok || !ipRes.row.Connected {
			continue
		}

		entries = append(entries, routeEntry{
			Prefix: prefix.Masked(),
			Alias:  ifRes.row.Alias(),
			Index:  int(ifRes.row.InterfaceIndex),
			Metric: addUint32Saturating(row.Metric, ipRes.row.Metric),
		})
	}

	m.routeTable.Store(buildRouteTable(entries, m.preferredInterfaces))
}
