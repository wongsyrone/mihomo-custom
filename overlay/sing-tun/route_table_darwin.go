//go:build darwin

package tun

import (
	"net"
	"net/netip"

	"github.com/metacubex/sing/common/control"
	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

func (m *defaultInterfaceMonitor) refreshRouteTable() {
	if m.underNetworkExtension || m.interfaceFinder == nil {
		return
	}
	ribMessage, err := route.FetchRIB(unix.AF_UNSPEC, route.RIBTypeRoute, 0)
	if err != nil {
		return
	}
	routeMessages, err := route.ParseRIB(route.RIBTypeRoute, ribMessage)
	if err != nil {
		return
	}

	type cachedIface struct {
		iface *control.Interface
		ok    bool
	}
	ifaces := make(map[int]cachedIface)
	resolveIface := func(index int) (*control.Interface, bool) {
		if index <= 0 {
			return nil, false
		}
		if res, cached := ifaces[index]; cached {
			return res.iface, res.ok
		}
		var res cachedIface
		ifObj, fErr := m.interfaceFinder.ByIndex(index)
		if fErr == nil && ifObj != nil && ifObj.Flags&net.FlagUp != 0 && ifObj.Flags&net.FlagLoopback == 0 && ifObj.Name != "" {
			res = cachedIface{iface: ifObj, ok: true}
		}
		ifaces[index] = res
		return res.iface, res.ok
	}

	var order uint32
	entries := make([]routeEntry, 0, len(routeMessages))
	for _, rawMsg := range routeMessages {
		rm, ok := rawMsg.(*route.RouteMessage)
		if !ok {
			continue
		}
		if rm.Flags&unix.RTF_UP == 0 || rm.Flags&(unix.RTF_REJECT|unix.RTF_BLACKHOLE) != 0 {
			continue
		}
		if len(rm.Addrs) <= unix.RTAX_DST {
			continue
		}

		var (
			addr netip.Addr
			ones int
		)
		switch dst := rm.Addrs[unix.RTAX_DST].(type) {
		case *route.Inet4Addr:
			addr = netip.AddrFrom4(dst.IP)
			if len(rm.Addrs) > unix.RTAX_NETMASK && rm.Addrs[unix.RTAX_NETMASK] != nil {
				mask, isMask := rm.Addrs[unix.RTAX_NETMASK].(*route.Inet4Addr)
				if !isMask {
					continue
				}
				var bits int
				ones, bits = net.IPMask(mask.IP[:]).Size()
				if ones <= 0 || bits != 32 {
					continue
				}
			} else if rm.Flags&unix.RTF_HOST != 0 {
				ones = 32
			} else {
				continue
			}
		case *route.Inet6Addr:
			// KAME encodes scope IDs in bytes 2..3 of fe80:: addresses.
			if dst.IP[0] == 0xfe && (dst.IP[1]&0xc0) == 0x80 {
				continue
			}
			addr = netip.AddrFrom16(dst.IP)
			if len(rm.Addrs) > unix.RTAX_NETMASK && rm.Addrs[unix.RTAX_NETMASK] != nil {
				mask, isMask := rm.Addrs[unix.RTAX_NETMASK].(*route.Inet6Addr)
				if !isMask {
					continue
				}
				var bits int
				ones, bits = net.IPMask(mask.IP[:]).Size()
				if ones <= 0 || bits != 128 {
					continue
				}
			} else if rm.Flags&unix.RTF_HOST != 0 {
				ones = 128
			} else {
				continue
			}
		default:
			continue
		}

		prefix := netip.PrefixFrom(addr, ones).Masked()
		if !isEligibleRoutePrefix(prefix) {
			continue
		}

		ifObj, valid := resolveIface(rm.Index)
		if !valid {
			continue
		}

		metric := order
		order++
		if rm.Flags&unix.RTF_IFSCOPE != 0 {
			metric += 1 << 30
		}

		entries = append(entries, routeEntry{
			Prefix: prefix,
			Alias:  ifObj.Name,
			Index:  ifObj.Index,
			Metric: metric,
		})
	}

	m.routeTable.Store(buildRouteTable(entries, m.preferredInterfaces))
}
