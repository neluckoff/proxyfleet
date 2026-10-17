# ProxyFleet

**Self-hosted control plane for distributed HTTP, SOCKS5, and Telegram WEB proxy infrastructure.**

ProxyFleet gives you one clean web interface for deploying proxy nodes on multiple VPS instances, creating independent authenticated proxies, and monitoring the health of the entire fleet.

## ✨ Highlights

- multiple nodes managed by one control plane;
- multiple HTTP, SOCKS5, or Telegram WEB proxies per node;
- encrypted Telegram WEB secrets with ready-to-use `tg://webproxy` links;
- outbound-only agent enrollment with one-time tokens;
- live health, traffic, connection, memory, and uptime metrics;
- encrypted storage and monitoring for third-party proxies;
- Telegram notifications with optional proxy support;
- installable PWA with per-device Web Push settings;
- English and Russian interface;
- update notices sourced from published GitHub Releases;
- no mandatory `.env` file;
- multi-arch images for `linux/amd64` and `linux/arm64`.

## 🚀 Recommended installation

```bash
sudo mkdir -p /opt/proxyfleet
sudo chown -R "$USER":"$(id -gn)" /opt/proxyfleet
cd /opt/proxyfleet

curl -fsSL https://raw.githubusercontent.com/neluckoff/proxyfleet/master/docker-compose.yml \
  -o docker-compose.yml

docker compose pull
docker compose up -d --no-build
docker compose ps
```

Open `http://127.0.0.1:8088` and complete the first-run wizard. ProxyFleet creates its session and encryption secrets automatically and stores persistent state in the `proxyfleet-control-data` volume.

For a public deployment, keep the container bound to localhost and place nginx, Caddy, or another HTTPS reverse proxy in front of it.

## 🛰️ Add a proxy node

Open **Nodes**, create a node, and run the generated Docker command on the target VPS. The same image starts in agent mode, so there is no separate node image to install.

```text
Control plane ── outbound HTTPS ──▶ Agent / VPS
                                      ├─ HTTP proxy
                                      ├─ SOCKS5 proxy
                                      └─ Telegram WEB proxy
```

## ♻️ Update

```bash
cd /opt/proxyfleet
docker compose pull
docker compose up -d --no-build --remove-orphans
docker image prune -f
```

The control-plane container is recreated while the persistent volume remains intact.

For reproducible deployments, pin `PROXYFLEET_IMAGE` to an exact release such as `neluckoff/proxyfleet:0.5.0` or to an immutable image digest shown on the **Tags** page.

## 🔐 Security notes

- Managed proxy passwords are stored only as hashes.
- Third-party proxy URLs are encrypted at rest.
- Admin routes require an authenticated owner session.
- Loopback, private, and link-local proxy destinations are blocked by default.
- Proxy ports should be restricted to trusted client IP addresses at the firewall.

## 🔗 Links

- [Documentation and source code](https://github.com/neluckoff/proxyfleet)
- [Issue tracker](https://github.com/neluckoff/proxyfleet/issues)
- [Security policy](https://github.com/neluckoff/proxyfleet/security/policy)
- [Available tags](https://hub.docker.com/r/neluckoff/proxyfleet/tags)

> The project is under active development. Review release notes and back up the `proxyfleet-control-data` volume before production upgrades.
