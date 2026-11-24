// This file is part of betabeta.
// Copyright (C) 2026  geckyzz <gecko.geckyzz@proton.me>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the top-level TOML configuration for the VPN controller.
type Config struct {
	Server     ServerConfig              `toml:"server"`
	Providers  map[string]ProviderConfig `toml:"providers"`
	Persistent PersistentConfig          `toml:"persistent"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Listen    string `toml:"listen"`
	AuthToken string `toml:"auth_token"`
	BasePort  int    `toml:"base_port"`
}

// ProviderConfig describes a VPN provider and its credentials.
type ProviderConfig struct {
	Enabled            bool   `toml:"enabled"`
	Priority           int    `toml:"priority"`
	VPNServiceProvider string `toml:"vpn_service_provider"`
	VPNType            string `toml:"vpn_type"`

	// WireGuard
	WireguardPrivateKey   string `toml:"wireguard_private_key"`
	WireguardAddresses    string `toml:"wireguard_addresses"`
	WireguardPresharedKey string `toml:"wireguard_preshared_key"`
	WireguardEndpointPort string `toml:"wireguard_endpoint_port"`

	// OpenVPN
	OpenVPNUser         string `toml:"openvpn_user"`
	OpenVPNPassword     string `toml:"openvpn_password"`
	OpenVPNProtocol     string `toml:"openvpn_protocol"`
	OpenVPNEndpointPort string `toml:"openvpn_endpoint_port"`

	Countries       []string `toml:"countries"`
	Socks5PortBase  int      `toml:"socks5_port_base"`
	ControlPortBase int      `toml:"control_port_base"`

	// Failover: providers to try next if this one fails for a country.
	FailoverTo []string `toml:"failover_to"`
}

// PersistentConfig lists countries whose containers stay alive.
type PersistentConfig struct {
	Countries []string `toml:"countries"`
}

// LoadConfig reads a TOML config file.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8880"
	}
	if cfg.Server.BasePort == 0 {
		cfg.Server.BasePort = 8881
	}
	return &cfg, nil
}

// IsPersistent returns true if the country should stay alive.
func (c *Config) IsPersistent(country string) bool {
	for _, pc := range c.Persistent.Countries {
		if strings.EqualFold(pc, country) {
			return true
		}
	}
	return false
}

// ProviderOrder returns providers sorted by priority (ascending).
func (c *Config) ProviderOrder() []string {
	type pv struct {
		name     string
		priority int
	}
	var list []pv
	for name, p := range c.Providers {
		if !p.Enabled {
			continue
		}
		list = append(list, pv{name: name, priority: p.Priority})
	}
	// Simple insertion sort by priority.
	for i := 1; i < len(list); i++ {
		key := list[i]
		j := i - 1
		for j >= 0 && list[j].priority > key.priority {
			list[j+1] = list[j]
			j--
		}
		list[j+1] = key
	}
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.name
	}
	return names
}

// Must have a config file path.
func ConfigPath() string {
	if p := os.Getenv("BETABETA_CONFIG"); p != "" {
		return p
	}
	return "config.toml"
}
