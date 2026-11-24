//go:build linux && !android

package tun

import (
	"net"
	"net/netip"

	"github.com/sagernet/netlink"
	"golang.org/x/sys/unix"
)

func (m *defaultInterfaceMonitor) refreshRouteTable() {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, &netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return
	}

	type cachedLink struct {
		attrs *netlink.LinkAttrs
		ok    bool
	}
	links := make(map[int]cachedLink)
	resolveLink := func(index int) (*netlink.LinkAttrs, bool) {
		if index <= 0 {
			return nil, false
		}
		if res, cached := links[index]; cached {
			return res.attrs, res.ok
		}
		var res cachedLink
		link, lErr := netlink.LinkByIndex(index)
		if lErr == nil && link != nil {
			attrs := link.Attrs()
			if attrs != nil && attrs.Flags&net.FlagUp != 0 && attrs.Flags&net.FlagLoopback == 0 && attrs.Name != "" {
				res = cachedLink{attrs: attrs, ok: true}
			}
		}
		links[index] = res
		return res.attrs, res.ok
	}

	entries := make([]routeEntry, 0, len(routes))
	for _, route := range routes {
		if route.Type != unix.RTN_UNICAST || route.Dst == nil {
			continue
		}
		ip, ok := netip.AddrFromSlice(route.Dst.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		ones, bits := route.Dst.Mask.Size()
		if ones <= 0 || bits != ip.BitLen() {
			continue
		}
		prefix := netip.PrefixFrom(ip, ones).Masked()
		if !isEligibleRoutePrefix(prefix) {
			continue
		}

		var metric uint32
		if route.Priority > 0 {
			metric = uint32(route.Priority)
		}

		if route.LinkIndex > 0 {
			if attrs, valid := resolveLink(route.LinkIndex); valid {
				entries = append(entries, routeEntry{
					Prefix: prefix,
					Alias:  attrs.Name,
					Index:  attrs.Index,
					Metric: metric,
				})
			}
			continue
		}

		for _, nh := range route.MultiPath {
			if nh == nil || nh.LinkIndex <= 0 {
				continue
			}
			if attrs, valid := resolveLink(nh.LinkIndex); valid {
				entries = append(entries, routeEntry{
					Prefix: prefix,
					Alias:  attrs.Name,
					Index:  attrs.Index,
					Metric: metric,
				})
			}
		}
	}

	m.routeTable.Store(buildRouteTable(entries, m.preferredInterfaces))
}
