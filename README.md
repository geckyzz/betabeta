# VPN Controller — on-demand Gluetun SOCKS5 proxy manager

A small Go service that starts, stops, and manages [Gluetun](https://github.com/qdm12/gluetun)
podman containers on demand, exposing them as SOCKS5 proxies over a simple HTTP API.

It was built for a multi-server setup where one host acts as a VPN bridge (reachable via
[Tailscale](https://tailscale.com/)) and other hosts download through it. Each target
country (Japan, Indonesia, Taiwan, Hong Kong, USA) gets its own Gluetun container, started
only when needed and torn down when idle.

```
client ──► VPN Controller ──► podman run qmcgaw/gluetun (ProtonVPN → Japan)
             HTTP API                    SOCKS5 proxy
                                         ▲
                                         │
                           your download client uses socks5://...
```

## What it does

1. Reads a TOML config file describing VPN providers, which countries each
   supports, and which countries should stay alive.
2. Exposes an HTTP API on port `8880`.
3. `POST /v1/acquire` — starts a Gluetun container for a country, waits for the VPN to
   come up, verifies the SOCKS5 proxy actually works by pinging an endpoint through it,
   then returns a `socks5://` URL.
4. `POST /v1/release` — stops and removes the container (unless the country is marked
   persistent).
5. Automatically fails over to the next provider if one can't bring up a country.
6. On startup, pulls the latest `qmcgaw/gluetun` image and purges unused images.

## Prerequisites (bridge host only)

You need one host to run the controller. That host must have:

- [podman](https://podman.io/) installed and working
- `/dev/net/tun` accessible (Gluetun needs `NET_ADMIN` + `tun`)
- A Tailscale network (or similar) so other hosts can reach the SOCKS5 ports
- [Go](https://go.dev/) 1.21+ (only needed to build the controller)

No other host needs the controller — they only need to reach `http://<bridge>:8880`.

## Quick start

### 1. Build the controller

With **make**:

```bash
git clone https://github.com/geckyzz/betabeta.git
cd betabeta
make build
```

Or with **[Taskfile](https://taskfile.dev)**:

```bash
task build
```

Or with plain Go:

```bash
go build -o betabeta ./src/
```

This produces a single `betabeta` binary.

### 2. Create the config

```bash
cp config.example.toml config.toml
# edit config.toml — see Configuration below
```

### 3. Run

```bash
make run
# or
./betabeta
```

The controller listens on `http://0.0.0.0:8880`. Bind it to localhost or Tailscale only
if you don't want it exposed on the public interface (it handles VPN credentials).

Any application that supports a SOCKS5 proxy can use the `proxy_url` returned by
`POST /v1/acquire`. See the API reference below for examples.

## Installation

betabeta can be installed per-user with **make** or **Taskfile**:

### With make

```bash
make install                    # binary → ~/.local/bin/betabeta, config → ~/.config/betabeta/
make systemd-install            # + systemd user service with dynamic paths
make uninstall                  # remove everything

### With Taskfile

```bash
task install                    # binary → ~/.local/bin/betabeta, config → ~/.config/betabeta/
task systemd-install            # + systemd user service with dynamic paths
task uninstall                  # remove everything
```

When `systemd-install` is used, the service file at `misc/betabeta.service` is
templated: `__BIN_PATH__`, `__CONFIG_DIR__`, and `__CONFIG_TOML__` are replaced
with your actual install paths (`~/.local/bin/betabeta`,
`~/.config/betabeta`, `~/.config/betabeta/config.toml`). The resulting service is
written to `~/.config/systemd/user/betabeta.service` — disabled by default, so it
will not start until you enable it:

```bash
systemctl --user daemon-reload
systemctl --user enable --now betabeta
```

## Configuration (`config.toml`)

The controller is configured entirely through a TOML file. It is platform- and
provider-agnostic: you can run any VPN provider and any country that Gluetun
supports. There are no hardcoded provider or country lists.

```toml
[server]
  listen = ":8880"
  auth_token = "CHANGE_ME_SECRET_TOKEN"
  base_port = 8881
```

| Field | Meaning |
|-------|---------|
| `listen` | HTTP listen address for the controller API (e.g. `:8880`, `127.0.0.1:8880`) |
| `auth_token` | Bearer token required on all API calls. Leave empty to disable auth (only safe if bound to localhost/Tailscale) |
| `base_port` | Starting SOCKS5 port. Each provider:country pair gets `base_port + N` |

### Providers

Each provider block under `[providers.<name>]` defines one VPN provider. The
`<name>` is an arbitrary identifier you choose — it is used in container names
(`vpn-<name>-<country>`) and failover references, but has no other significance.

```toml
[providers.myprovider]
  enabled = true
  priority = 1
  vpn_service_provider = "protonvpn"   # any Gluetun-supported provider name
  vpn_type = "wireguard"                # "wireguard" or "openvpn"

  # WireGuard settings
  wireguard_private_key = "BASE64_KEY"
  wireguard_addresses = "10.x.y.z/32"   # optional
  wireguard_preshared_key = ""          # optional
  wireguard_endpoint_port = ""          # optional

  # OpenVPN settings (used when vpn_type = "openvpn")
  openvpn_user = ""                     # optional
  openvpn_password = ""                 # optional
  openvpn_protocol = "udp"              # optional
  openvpn_endpoint_port = ""            # optional

  # Countries this provider can serve. Any ISO 3166-1 alpha-2 code Gluetun
  # supports for this provider works — there is no hardcoded country list.
  countries = ["jp", "id", "tw", "hk", "us"]

  # Port allocation
  socks5_port_base = 8881               # base port for SOCKS5 proxies
  control_port_base = 8101              # base port for Gluetun control API (localhost only)

  # Failover: provider names to try if this one cannot bring up a country.
  # References [providers.<name>] keys. Optional.
  failover_to = ["mybackup"]
```

| Field | Meaning |
|-------|---------|
| `enabled` | whether this provider is used |
| `priority` | lower = tried first during failover |
| `vpn_service_provider` | any Gluetun-supported provider name (e.g. `protonvpn`, `windscribe`, `nordvpn`, `mullvad`, `cyberghost`, `surfshark`, `custom`, …) |
| `vpn_type` | `wireguard` or `openvpn` |
| `wireguard_private_key` | WireGuard private key (base64). Required when `vpn_type = "wireguard"` |
| `wireguard_addresses` | optional WireGuard interface address |
| `wireguard_preshared_key` | optional WireGuard preshared key |
| `wireguard_endpoint_port` | optional WireGuard endpoint port |
| `openvpn_user` / `openvpn_password` | OpenVPN credentials, required when `vpn_type = "openvpn"` |
| `openvpn_protocol` | optional OpenVPN protocol (`udp` or `tcp`) |
| `openvpn_endpoint_port` | optional OpenVPN endpoint port |
| `countries` | country codes this provider can serve — any ISO 3166-1 alpha-2 code Gluetun supports for this provider |
| `socks5_port_base` | base port for this provider's SOCKS5 proxies |
| `control_port_base` | base port for this provider's Gluetun control API (bound to localhost only) |
| `failover_to` | provider names (`[providers.<name>]` keys) to try if this one fails for a country |

> **Tip:** To add a new provider, just add another `[providers.<name>]` block with its credentials and the countries it supports. To add a new country, add its ISO code to a provider's `countries` list.

### Persistent countries

Countries listed under `[persistent]` keep their container running even when idle,
so the next use reuses it immediately:

```toml
[persistent]
  countries = ["id", "tw"]
```

Any country code can be listed here — there is no predefined set. Non-persistent
countries have their container stopped and removed after each use.

### Example full config (illustrative only)

```toml
[server]
  listen = ":8880"
  auth_token = "CHANGE_ME"
  base_port = 8881

[providers.protonvpn]
  enabled = true
  priority = 1
  vpn_service_provider = "protonvpn"
  vpn_type = "wireguard"
  wireguard_private_key = "BASE64_PRIVATE_KEY"
  countries = ["jp", "id", "tw", "hk", "us"]
  socks5_port_base = 8881
  control_port_base = 8101
  failover_to = ["windscribe"]

[providers.windscribe]
  enabled = true
  priority = 2
  vpn_service_provider = "windscribe"
  vpn_type = "wireguard"
  wireguard_private_key = "BASE64_PRIVATE_KEY"
  wireguard_addresses = "10.64.x.y/32"
  wireguard_preshared_key = "PRESHARED_KEY"
  countries = ["jp", "us"]
  socks5_port_base = 8891
  control_port_base = 8111

[persistent]
  countries = ["id", "tw"]
```

Replace the provider names, keys, and country lists with your own. Gluetun's
supported providers and countries are documented in the [Gluetun wiki](https://github.com/qdm12/gluetun-wiki).

Get VPN credentials from your provider's dashboard. The exact field names
depend on the provider — see Gluetun's provider documentation for each one.

## API reference

All endpoints require `Authorization: Bearer <auth_token>` unless `auth_token` is empty.

| Method | Endpoint | Body | Response |
|--------|----------|------|----------|
| `POST` | `/v1/acquire` | `{"country": "jp"}` | `{"country":"jp","provider":"protonvpn","proxy_url":"socks5://0.0.0.0:8881","container":"vpn-protonvpn-jp","socks_port":8881}` |
| `POST` | `/v1/release` | `{"country": "jp"}` | `{"released":"jp"}` |
| `GET` | `/v1/status` | — | map of country → `{refcount, provider, proxy_url, container}` |
| `GET` | `/v1/health` | — | `{"status":"ok"}` |

If no provider can bring up the country, `acquire` returns HTTP 503 with `{"error": "..."}`.

### Example: acquire a proxy

```bash
curl -s -X POST http://bridge:8880/v1/acquire \
  -H "Authorization: Bearer CHANGE_ME" \
  -H "Content-Type: application/json" \
  -d '{"country":"jp"}'
# → {"country":"jp","provider":"protonvpn","proxy_url":"socks5://0.0.0.0:8881","container":"vpn-protonvpn-jp","socks_port":8881}
```

### Example: release a proxy

```bash
curl -s -X POST http://bridge:8880/v1/release \
  -H "Authorization: Bearer CHANGE_ME" \
  -H "Content-Type: application/json" \
  -d '{"country":"jp"}'
# → {"released":"jp"}
```

Any application that supports a SOCKS5 proxy can then use the returned `proxy_url`.

## Provider priority and failover

The controller tries providers in ascending `priority` order. If a provider
cannot bring up a requested country (or its connectivity test fails), it
automatically falls through to the next provider listed in `failover_to`, then
to the next provider in priority order. Set `failover_to` in each provider
block to define your own fallback chain.

There is no built-in preference for any specific provider — the order is
entirely determined by your `priority` and `failover_to` settings.

## systemd user service

A user service file is included at `misc/betabeta.service`. Install it without
starting:

```bash
cp misc/betabeta.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable betabeta   # enable, but don't start yet
```

When you're ready to run it:

```bash
systemctl --user start betabeta
```

## License

GPLv3-or-later. See [LICENSE](./LICENSE) for the full text.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

## Repository layout

```
betabeta/
├── LICENSE                 # GPLv3-or-later license
├── README.md               # this file
├── .gitignore              # excludes betabeta binary and build artifacts
├── Makefile                # build / install / uninstall targets
├── Taskfile.yaml           # Taskfile runner tasks (alternative to make)
├── config.example.toml     # example configuration (copy to config.toml)
├── go.mod / go.sum         # Go module definition
├── src/
│   ├── main.go             # HTTP server entrypoint + image ensure on startup
│   ├── config.go           # TOML config loading
│   ├── manager.go          # container lifecycle, refcounting, failover
│   ├── podman.go           # podman create/start/stop/rm wrappers
│   ├── api.go              # HTTP handlers (/v1/acquire, /v1/release, /v1/status)
│   └── image.go            # pull latest gluetun image + purge unused images
│   └── manager_test.go     # unit tests
├── misc/
│   └── betabeta.service   # systemd user service
└── betabeta          # compiled binary (after `go build`, gitignored)
```
