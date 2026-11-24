package sing_tun

import (
	"net/netip"

	tun "github.com/metacubex/sing-tun"
	"github.com/metacubex/sing/common/control"
)

type routeInterfaceFinder interface {
	RouteInterface(destination netip.Addr, excludeInterface string) *control.Interface
}

func (d *cDialerInterfaceFinder) routeInterfaceName(destination netip.Addr) string {
	if d == nil || d.defaultInterfaceMonitor == nil {
		return ""
	}
	return routeInterfaceNameFromMonitor(d.defaultInterfaceMonitor, d.tunName, destination)
}

func routeInterfaceNameFromMonitor(monitor tun.DefaultInterfaceMonitor, tunName string, destination netip.Addr) string {
	if monitor == nil {
		return ""
	}
	rf, ok := monitor.(routeInterfaceFinder)
	if !ok {
		return ""
	}
	netInterface := rf.RouteInterface(destination, tunName)
	if netInterface == nil || netInterface.Name == "" || netInterface.Name == tunName {
		return ""
	}
	return netInterface.Name
}

func routeInterfaceForAddr(monitor tun.DefaultInterfaceMonitor, tunName string, destination netip.Addr) string {
	if name := routeInterfaceNameFromMonitor(monitor, tunName, destination); name != "" {
		return name
	}
	if netInterface, _ := DefaultInterfaceFinder.ByAddr(destination); netInterface != nil && netInterface.Name != "" && netInterface.Name != tunName {
		return netInterface.Name
	}
	return ""
}
