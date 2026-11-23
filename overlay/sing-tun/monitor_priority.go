// Problem:
// On multi-homed Windows hosts using Automatic Metric, multiple active adapters
// (e.g., Wi-Fi and wired Ethernet) frequently hold 0.0.0.0/0 default routes with
// equal or close combined metrics (row.Metric + iface.Metric). Minor link-speed
// changes or virtual switch updates trigger checkUpdate() oscillations, and
// equal-metric ties previously depended on OS route table iteration order.
//
// Solution:
// Deduplicate default-route candidates per interface index by lowest metric and
// select deterministically: honor PreferredInterfaces order first (ignoring
// metric jitter), then fall back to lowest combined metric with equal-metric
// stickiness to the currently active interface name.

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
