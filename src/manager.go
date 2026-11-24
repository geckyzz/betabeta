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
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// ProxyInfo describes an acquired SOCKS5 proxy.
type ProxyInfo struct {
	Country     string `json:"country"`
	Provider    string `json:"provider"`
	ProxyURL    string `json:"proxy_url"`
	Container   string `json:"container"`
	SocksPort   int    `json:"socks_port"`
	ControlPort int    `json:"control_port"`
}

// defaultSocksPortBase is used when a provider leaves socks5_port_base unset;
// a zero host port can never be dialed by waitForVPN.
const defaultSocksPortBase = 20000

// Manager owns the lifecycle of VPN containers.
type Manager struct {
	cfg          *Config
	mu           sync.Mutex
	refs         map[string]int        // country -> refcount
	active       map[string]*ProxyInfo // country -> current proxy info
	ports        map[string]int        // "provider:country" -> assigned socks port
	controlPorts map[string]int        // "provider:country" -> assigned control port
}

// NewManager creates a Manager from config.
func NewManager(cfg *Config) *Manager {
	return &Manager{
		cfg:          cfg,
		refs:         make(map[string]int),
		active:       make(map[string]*ProxyInfo),
		ports:        make(map[string]int),
		controlPorts: make(map[string]int),
	}
}

// countryKey normalizes a country code to lowercase.
func countryKey(country string) string {
	return strings.ToLower(strings.TrimSpace(country))
}

// portFor returns the SOCKS5 host port for a provider+country pair,
// allocating the first free port at or above the provider's configured base.
// Because every allocation scans the full set of in-use ports, ports are
// unique across providers even when their bases overlap.
func (m *Manager) portFor(provider, country string, cfg ProviderConfig) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := provider + ":" + countryKey(country)
	if p, ok := m.ports[key]; ok {
		return p
	}
	used := m.usedPortsLocked()
	port := cfg.Socks5PortBase
	if port == 0 {
		port = defaultSocksPortBase
	}
	for used[port] {
		port++
	}
	m.ports[key] = port
	return port
}

// controlPortFor returns the localhost control-server port for a
// provider+country pair, allocating the first free port at or above the
// provider's configured base so it cannot collide with any other host port.
func (m *Manager) controlPortFor(provider, country string, cfg ProviderConfig) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := provider + ":" + countryKey(country)
	if p, ok := m.controlPorts[key]; ok {
		return p
	}
	used := m.usedPortsLocked()
	base := cfg.ControlPortBase
	if base == 0 {
		base = 8000
	}
	port := base
	for used[port] {
		port++
	}
	m.controlPorts[key] = port
	return port
}

// usedPortsLocked returns every host port currently reserved by the manager
// (socks and control ports alike). The caller must hold m.mu.
func (m *Manager) usedPortsLocked() map[int]bool {
	used := make(map[int]bool, len(m.ports)+len(m.controlPorts))
	for _, p := range m.ports {
		used[p] = true
	}
	for _, p := range m.controlPorts {
		used[p] = true
	}
	return used
}

// testSocks5 checks if a SOCKS5 proxy is accepting TCP connections.
func testSocks5(hostPort string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", hostPort, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitForVPN polls the SOCKS5 port until it accepts connections or timeout.
func waitForVPN(hostPort string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if testSocks5(hostPort, 2*time.Second) {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// proxyTestURLs are tried, in order, through the proxy to check connectivity
// to the internet. web-api.nordvpn.com is kept as the first endpoint (mirrors
// yt-dlp's test_connection); generic endpoints cover other providers whose
// traffic to NordVPN's API may not work.
var proxyTestURLs = []string{
	"https://web-api.nordvpn.com/v1/ips/info",
	"https://example.com",
	"https://www.google.com",
}

// testProxyViaSocks5 verifies that a SOCKS5 proxy can actually reach the
// internet by fetching endpoints through it. Returns true if any endpoint
// returns HTTP 200 through the proxy.
func testProxyViaSocks5(socksHostPort string, timeout time.Duration) bool {
	dialer, err := proxy.SOCKS5("tcp", socksHostPort, nil, proxy.Direct)
	if err != nil {
		prnv("SOCKS5 dialer creation failed for %s: %v", socksHostPort, err)
		return false
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	for _, url := range proxyTestURLs {
		resp, err := client.Get(url)
		if err != nil {
			prnv("Proxy test GET through %s to %s failed: %v", socksHostPort, url, err)
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			prnv("✓ Proxy connectivity test passed via %s (%s)", socksHostPort, url)
			return true
		}
		prnv("Proxy test through %s to %s returned HTTP %d", socksHostPort, url, resp.StatusCode)
	}
	prnv("Proxy connectivity test through %s failed for all endpoints", socksHostPort)
	return false
}

// Acquire obtains a SOCKS5 proxy for the given country, trying providers in
// priority order with failover. Increments the refcount for the country.
func (m *Manager) Acquire(country string) (*ProxyInfo, error) {
	m.mu.Lock()
	ck := countryKey(country)
	m.refs[ck]++
	if existing, ok := m.active[ck]; ok {
		// Already have an active proxy for this country.
		m.mu.Unlock()
		return existing, nil
	}
	m.mu.Unlock()

	providerOrder := m.cfg.ProviderOrder()
	inQueue := make(map[string]bool, len(providerOrder))
	for _, p := range providerOrder {
		inQueue[p] = true
	}

	var (
		lastErr      error
		supportedAny bool
	)
	// Iterate over a slice that may grow: when a provider fails, its
	// failover_to targets are appended to the end of the list.
	for i := 0; i < len(providerOrder); i++ {
		provider := providerOrder[i]
		pCfg := m.cfg.Providers[provider]

		supported := false
		for _, c := range pCfg.Countries {
			if strings.EqualFold(c, ck) {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}
		supportedAny = true

		proxy, err := m.tryProvider(provider, pCfg, ck)
		if err != nil {
			lastErr = err
			prnv("provider %s failed for country %s: %v", provider, ck, err)
			for _, f := range pCfg.FailoverTo {
				if _, ok := m.cfg.Providers[f]; !ok || !m.cfg.Providers[f].Enabled || inQueue[f] {
					continue
				}
				inQueue[f] = true
				providerOrder = append(providerOrder, f)
			}
			continue
		}

		m.mu.Lock()
		m.active[ck] = proxy
		m.mu.Unlock()
		prni("✓ Acquired VPN proxy for %s via %s (%s)", ck, provider, proxy.ProxyURL)
		return proxy, nil
	}

	decrement := func() {
		m.mu.Lock()
		m.refs[ck]--
		if m.refs[ck] <= 0 {
			delete(m.refs, ck)
		}
		m.mu.Unlock()
	}

	if !supportedAny {
		decrement()
		return nil, fmt.Errorf("no enabled provider supports country %s", ck)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no VPN provider could serve country %s", ck)
	}
	decrement()
	return nil, fmt.Errorf("all VPN providers failed for country %s: %w", ck, lastErr)
}

// tryProvider attempts to start a container for a provider+country and
// verify the SOCKS5 proxy is working.
func (m *Manager) tryProvider(provider string, cfg ProviderConfig, country string) (*ProxyInfo, error) {
	name := fmt.Sprintf("vpn-%s-%s", provider, country)
	socksPort := m.portFor(provider, country, cfg)
	controlPort := m.controlPortFor(provider, country, cfg)
	hostPort := net.JoinHostPort("0.0.0.0", strconv.Itoa(socksPort))

	if err := podmanCreateAndStart(name, provider, country, cfg, socksPort, controlPort); err != nil {
		return nil, fmt.Errorf("create/start: %w", err)
	}

	if !waitForVPN(hostPort, 60*time.Second) {
		m.stopAndMaybeRemove(name, country)
		return nil, fmt.Errorf("SOCKS5 proxy on port %d did not become ready", socksPort)
	}

	// Verify the proxy can actually reach the internet before handing it out.
	// If it can't, tear down and let failover try the next provider.
	if !testProxyViaSocks5(hostPort, 20*time.Second) {
		m.stopAndMaybeRemove(name, country)
		return nil, fmt.Errorf("SOCKS5 proxy on port %d failed connectivity test", socksPort)
	}

	return &ProxyInfo{
		Country:     country,
		Provider:    provider,
		ProxyURL:    "socks5://" + hostPort,
		Container:   name,
		SocksPort:   socksPort,
		ControlPort: controlPort,
	}, nil
}

// stopAndMaybeRemove stops and removes a container when the country is not
// persistent. Lifecycle operations are serialized with podmanMu so they
// cannot race a concurrent create/start on the same container name.
func (m *Manager) stopAndMaybeRemove(name, country string) {
	podmanMu.Lock()
	defer podmanMu.Unlock()
	_ = podmanStop(name)
	if !m.cfg.IsPersistent(country) {
		_ = podmanRemove(name)
	}
}

// Release decrements the refcount for a country. If it reaches zero and the
// country is not persistent, the container is stopped and removed.
func (m *Manager) Release(country string) error {
	m.mu.Lock()
	ck := countryKey(country)
	if m.refs[ck] > 0 {
		m.refs[ck]--
	}
	ref := m.refs[ck]
	proxy, isActive := m.active[ck]
	m.mu.Unlock()

	if ref > 0 {
		prnv("Country %s still has %d ref(s), keeping container", country, ref)
		return nil
	}

	if !isActive {
		return nil
	}

	// Refcount is zero and there is an active container.
	if m.cfg.IsPersistent(country) {
		// Keep the container alive, just drop our tracking.
		m.mu.Lock()
		delete(m.active, ck)
		delete(m.refs, ck)
		m.mu.Unlock()
		prni("✓ Country %s is persistent; container %s kept running", country, proxy.Container)
		return nil
	}

	// Stop and remove the container.
	m.mu.Lock()
	delete(m.active, ck)
	delete(m.refs, ck)
	m.mu.Unlock()

	podmanMu.Lock()
	if err := podmanStop(proxy.Container); err != nil {
		prnv("Warning: failed to stop container %s: %v", proxy.Container, err)
	}
	if err := podmanRemove(proxy.Container); err != nil {
		prnv("Warning: failed to remove container %s: %v", proxy.Container, err)
	}
	podmanMu.Unlock()
	prni("✓ Released VPN container %s", proxy.Container)
	return nil
}

// Status returns the current state of all managed countries.
func (m *Manager) Status() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]interface{})
	for ck, ref := range m.refs {
		entry := map[string]interface{}{
			"refcount": ref,
		}
		if p, ok := m.active[ck]; ok {
			entry["provider"] = p.Provider
			entry["proxy_url"] = p.ProxyURL
			entry["container"] = p.Container
		}
		result[ck] = entry
	}
	return result
}
