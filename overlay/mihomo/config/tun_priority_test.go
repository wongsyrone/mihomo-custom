package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	LC "github.com/metacubex/mihomo/listener/config"
)

func TestTunDefaultInterfacePriorityCompat(t *testing.T) {
	t.Run("official config without custom field", func(t *testing.T) {
		rawCfg, err := UnmarshalRawConfig([]byte("tun:\n  enable: true\n  auto-route: true\n  auto-detect-interface: true\n"))
		if err != nil {
			t.Fatalf("UnmarshalRawConfig failed: %v", err)
		}
		if len(rawCfg.Tun.DefaultInterfacePriority) != 0 {
			t.Fatalf("expected empty DefaultInterfacePriority, got %v", rawCfg.Tun.DefaultInterfacePriority)
		}
		encoded, err := json.Marshal(rawCfg.Tun)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		if strings.Contains(string(encoded), "default-interface-priority") {
			t.Fatalf("expected omitempty JSON output without default-interface-priority, got %s", encoded)
		}
	})

	t.Run("custom config preserves priority order", func(t *testing.T) {
		rawCfg, err := UnmarshalRawConfig([]byte("tun:\n  enable: true\n  default-interface-priority:\n    - Wi-Fi\n    - Ethernet 2\n"))
		if err != nil {
			t.Fatalf("UnmarshalRawConfig failed: %v", err)
		}
		want := []string{"Wi-Fi", "Ethernet 2"}
		if !reflect.DeepEqual(rawCfg.Tun.DefaultInterfacePriority, want) {
			t.Fatalf("got %v, want %v", rawCfg.Tun.DefaultInterfacePriority, want)
		}
	})

	t.Run("Tun.Equal compares priority order and treats nil and empty identically", func(t *testing.T) {
		a := LC.Tun{Enable: true}
		b := LC.Tun{Enable: true, DefaultInterfacePriority: []string{}}
		if !a.Equal(b) {
			t.Fatal("expected nil and empty DefaultInterfacePriority to be equal")
		}

		c := LC.Tun{Enable: true, DefaultInterfacePriority: []string{"Wi-Fi", "Ethernet 2"}}
		d := LC.Tun{Enable: true, DefaultInterfacePriority: []string{"Ethernet 2", "Wi-Fi"}}
		if c.Equal(d) {
			t.Fatal("expected reordered DefaultInterfacePriority not to be equal")
		}
	})
}
