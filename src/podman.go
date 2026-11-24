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
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// podmanMu serializes lifecycle operations on containers so that concurrent
// Acquire calls cannot race on the same container name.
var podmanMu sync.Mutex

// podmanCmd runs a podman command and returns stdout, stderr, and exit code.
func podmanCmd(args ...string) (string, string, int) {
	cmd := exec.Command("podman", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return stdout.String(), stderr.String(), code
}

// podmanContainerExists checks if a container (running or stopped) exists.
func podmanContainerExists(name string) bool {
	_, _, code := podmanCmd("container", "exists", name)
	return code == 0
}

// podmanContainerRunning checks if a container is currently running.
func podmanContainerRunning(name string) bool {
	// No ^...$ anchors: whether podman's name filter matches regexes or
	// substrings, this is robust; the output check below confirms the name.
	out, _, code := podmanCmd("ps", "--filter", "name="+name, "--format", "{{.Names}}")
	if code != 0 {
		return false
	}
	return strings.Contains(out, name)
}

// podmanCreate creates a gluetun container for a provider+country.
func podmanCreate(name, provider, country string, cfg ProviderConfig, socksPort, controlPort int) error {
	args := []string{
		"create",
		"--name", name,
		"--cap-add", "NET_ADMIN",
		"--device", "/dev/net/tun:/dev/net/tun",
		"--restart", "unless-stopped",
		"-p", fmt.Sprintf("%d:1080/tcp", socksPort),
		"-p", fmt.Sprintf("127.0.0.1:%d:8000/tcp", controlPort),
	}

	env := []string{
		fmt.Sprintf("VPN_SERVICE_PROVIDER=%s", cfg.VPNServiceProvider),
		fmt.Sprintf("VPN_TYPE=%s", cfg.VPNType),
		fmt.Sprintf("SERVER_COUNTRIES=%s", country),
		fmt.Sprintf("SOCKS5_ENABLED=on"),
		fmt.Sprintf("SOCKS5_LISTENING_ADDRESS=:1080"),
		fmt.Sprintf("HTTP_CONTROL_SERVER_AUTH_DEFAULT_ROLE=%s", `{"auth":"apikey","apikey":"gluetun-controller"}`),
	}

	switch cfg.VPNType {
	case "wireguard":
		env = append(env,
			fmt.Sprintf("WIREGUARD_PRIVATE_KEY=%s", cfg.WireguardPrivateKey),
		)
		if cfg.WireguardAddresses != "" {
			env = append(env, fmt.Sprintf("WIREGUARD_ADDRESSES=%s", cfg.WireguardAddresses))
		}
		if cfg.WireguardPresharedKey != "" {
			env = append(env, fmt.Sprintf("WIREGUARD_PRESHARED_KEY=%s", cfg.WireguardPresharedKey))
		}
		if cfg.WireguardEndpointPort != "" {
			env = append(env, fmt.Sprintf("WIREGUARD_ENDPOINT_PORT=%s", cfg.WireguardEndpointPort))
		}
	case "openvpn":
		env = append(env,
			fmt.Sprintf("OPENVPN_USER=%s", cfg.OpenVPNUser),
			fmt.Sprintf("OPENVPN_PASSWORD=%s", cfg.OpenVPNPassword),
		)
		if cfg.OpenVPNProtocol != "" {
			env = append(env, fmt.Sprintf("OPENVPN_PROTOCOL=%s", cfg.OpenVPNProtocol))
		}
		if cfg.OpenVPNEndpointPort != "" {
			env = append(env, fmt.Sprintf("OPENVPN_ENDPOINT_PORT=%s", cfg.OpenVPNEndpointPort))
		}
	}

	for _, e := range env {
		args = append(args, "-e", e)
	}

	args = append(args, "qmcgaw/gluetun")

	_, stderr, code := podmanCmd(args...)
	if code != 0 {
		return fmt.Errorf("podman create failed (code %d): %s", code, stderr)
	}
	return nil
}

// podmanStart starts a container.
func podmanStart(name string) error {
	_, stderr, code := podmanCmd("start", name)
	if code != 0 {
		return fmt.Errorf("podman start failed (code %d): %s", code, stderr)
	}
	return nil
}

// podmanStop stops a container.
func podmanStop(name string) error {
	_, stderr, code := podmanCmd("stop", "--time", "10", name)
	if code != 0 {
		return fmt.Errorf("podman stop failed (code %d): %s", code, stderr)
	}
	return nil
}

// podmanRemove removes a container.
func podmanRemove(name string) error {
	_, stderr, code := podmanCmd("rm", "-f", name)
	if code != 0 {
		return fmt.Errorf("podman rm failed (code %d): %s", code, stderr)
	}
	return nil
}

// podmanCreateAndStart creates and starts a gluetun container. The
// check-then-create-then-start sequence runs under podmanMu so concurrent
// acquires of the same country cannot race (e.g. both seeing the container
// missing and the second `podman create` failing with a name-in-use error).
func podmanCreateAndStart(name, provider, country string, cfg ProviderConfig, socksPort, controlPort int) error {
	podmanMu.Lock()
	defer podmanMu.Unlock()
	if podmanContainerExists(name) {
		// Already exists; just start it if not running.
		if !podmanContainerRunning(name) {
			return podmanStart(name)
		}
		return nil
	}
	if err := podmanCreate(name, provider, country, cfg, socksPort, controlPort); err != nil {
		return err
	}
	return podmanStart(name)
}
