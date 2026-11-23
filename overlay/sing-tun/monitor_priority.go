// Problem:
// On multi-homed hosts, multiple active adapters (e.g., Wi-Fi and wired
// Ethernet) frequently hold default routes with equal or competing metrics
// (Windows Automatic Metric row.Metric + iface.Metric, Linux netlink route
// Priority, or macOS BSD RIB order / RTF_IFSCOPE). Minor link updates or route
// table iteration order trigger checkUpdate() oscillations or pick an undesired
// adapter over the user's preferred interface.
//
// Solution:
// Deduplicate default-route candidates per interface index by lowest metric and
// select deterministically across Windows, Linux, and macOS: honor
// PreferredInterfaces order first (ignoring metric jitter or scoped flags),
// then fall back to lowest metric with equal-metric stickiness to the currently
// active interface name.

//go:build linux || windows || darwin

package tun

type defaultRouteCandidate struct {
	Alias  string
	Index  int
	Metric uint32
}

type defaultRouteSelector struct {
	candidates []defaultRouteCandidate
	indexPos   map[int]int
}

func (s *defaultRouteSelector) add(alias string, index int, metric uint32) {
	if s.indexPos == nil {
		s.indexPos = make(map[int]int)
	}
	if pos, exists := s.indexPos[index]; exists {
		if metric < s.candidates[pos].Metric {
			s.candidates[pos].Metric = metric
			s.candidates[pos].Alias = alias
		}
		return
	}
	s.indexPos[index] = len(s.candidates)
	s.candidates = append(s.candidates, defaultRouteCandidate{
		Alias:  alias,
		Index:  index,
		Metric: metric,
	})
}

func (s *defaultRouteSelector) selectFor(m *defaultInterfaceMonitor) (string, int, bool) {
	var preferred []string
	currentName := ""
	if m != nil {
		preferred = m.preferredInterfaces
		currentName = m.currentDefaultInterfaceName()
	}
	selected, ok := selectDefaultInterface(s.candidates, preferred, currentName)
	if !ok {
		return "", 0, false
	}
	return selected.Alias, selected.Index, true
}

func filterPreferredInterfaces(preferred []string) []string {
	var filtered []string
	for _, name := range preferred {
		if name != "" {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func (m *defaultInterfaceMonitor) currentDefaultInterfaceName() string {
	if current := m.defaultInterface.Load(); current != nil {
		return current.Name
	}
	return ""
}

func selectDefaultInterface(candidates []defaultRouteCandidate, preferred []string, currentName string) (defaultRouteCandidate, bool) {
	if len(candidates) == 0 {
		return defaultRouteCandidate{}, false
	}

	if len(preferred) > 0 {
		for _, name := range preferred {
			if name == "" {
				continue
			}
			for _, c := range candidates {
				if c.Alias == name {
					return c, true
				}
			}
		}
	}

	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.Metric < best.Metric {
			best = c
		} else if c.Metric == best.Metric && currentName != "" && c.Alias == currentName {
			best = c
		}
	}
	return best, true
}
