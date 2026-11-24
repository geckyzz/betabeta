package main

import (
	"testing"
)

// TestPortAllocationUnique verifies that socks and control ports are unique
// across every provider and country, and that allocation is stable on repeat
// lookups. Uses the example config values from config.example.toml.
func TestPortAllocationUnique(t *testing.T) {
	cfg := &Config{Providers: map[string]ProviderConfig{
		"protonvpn": {
			Enabled:         true,
			Socks5PortBase:  8881,
			ControlPortBase: 8101,
			Countries:       []string{"jp", "id", "tw", "hk", "us"},
		},
		"windscribe": {
			Enabled:         true,
			Socks5PortBase:  8891,
			ControlPortBase: 8111,
			Countries:       []string{"jp", "us"},
		},
		"nordvpn": {
			Enabled:         true,
			Socks5PortBase:  8901,
			ControlPortBase: 8121,
			Countries:       []string{"jp", "id", "tw", "hk", "us"},
		},
	}}
	m := NewManager(cfg)

	type key struct{ provider, country string }
	seen := map[int]string{}
	allocated := map[key]int{}

	for _, provider := range []string{"protonvpn", "windscribe", "nordvpn"} {
		for _, country := range cfg.Providers[provider].Countries {
			pCfg := cfg.Providers[provider]
			socksPort := m.portFor(provider, country, pCfg)
			controlPort := m.controlPortFor(provider, country, pCfg)
			for _, port := range []int{socksPort, controlPort} {
				owner, dup := seen[port]
				if dup {
					t.Errorf("port %d allocated for both %s and %s", port, owner, provider+":"+country)
				}
				seen[port] = provider + ":" + country
			}
			allocated[key{provider, country}] = socksPort
		}
	}

	// Allocation must be stable across repeat lookups.
	for k, want := range allocated {
		if got := m.portFor(k.provider, k.country, cfg.Providers[k.provider]); got != want {
			t.Errorf("portFor(%s, %s) = %d, want %d", k.provider, k.country, got, want)
		}
	}
}

// TestPortAllocationUniqueSharedBase verifies that even when two providers
// share the same socks5_port_base, their allocated ports do not collide.
func TestPortAllocationUniqueSharedBase(t *testing.T) {
	cfg := &Config{Providers: map[string]ProviderConfig{
		"protonvpn": {
			Enabled:        true,
			Socks5PortBase: 8881,
			Countries:      []string{"jp", "us"},
		},
		"windscribe": {
			Enabled:        true,
			Socks5PortBase: 8881,
			Countries:      []string{"jp"},
		},
	}}
	m := NewManager(cfg)

	seen := map[int]string{}
	for _, provider := range []string{"protonvpn", "windscribe"} {
		for _, country := range cfg.Providers[provider].Countries {
			socksPort := m.portFor(provider, country, cfg.Providers[provider])
			if owner, dup := seen[socksPort]; dup {
				t.Errorf("socks port %d allocated for both %s and %s", socksPort, owner, provider+":"+country)
			}
			seen[socksPort] = provider + ":" + country
		}
	}
}
