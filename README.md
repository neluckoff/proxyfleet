<p align="center">
  <img src="internal/control/web/logo.png" width="112" alt="ProxyFleet logo">
</p>

<h1 align="center">ProxyFleet</h1>

<p align="center">
  A self-hosted control plane for deploying and managing distributed HTTP and SOCKS5 proxy nodes.
</p>

<p align="center">
  <a href="https://github.com/neluckoff/proxyfleet/actions/workflows/ci.yml"><img src="https://github.com/neluckoff/proxyfleet/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/neluckoff/proxyfleet/actions/workflows/docker-publish.yml"><img src="https://github.com/neluckoff/proxyfleet/actions/workflows/docker-publish.yml/badge.svg" alt="Docker publish"></a>
  <a href="https://hub.docker.com/r/neluckoff/proxyfleet"><img src="https://img.shields.io/docker/pulls/neluckoff/proxyfleet?logo=docker&label=Docker%20Pulls" alt="Docker pulls"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white" alt="Go 1.23+"></a>
</p>

<p align="center">
  <a href="#-quick-start-recommended">Quick start</a> ·
  <a href="#-features">Features</a> ·
  <a href="#%EF%B8%8F-architecture">Architecture</a> ·
  <a href="#-updating">Updating</a> ·
  <a href="#-contributing">Contributing</a>
</p>

ProxyFleet keeps a private proxy fleet understandable and manageable from one web interface. A single control plane stores the desired configuration, lightweight agents connect outbound from your VPS instances, and each node can serve multiple independent authenticated proxies.

There is no required `.env` file. The owner account, password, public URL, session secret, encryption key, and Web Push keys are created or configured during first-run setup and stored in a persistent Docker volume.

> [!IMPORTANT]
> Docker Hub is the recommended installation source. Building from the GitHub repository is fully supported for development, auditing, and custom images.

## ✨ Features

- one control plane for multiple proxy nodes;
- multiple independent proxies on a single node;
- authenticated HTTP proxy with `CONNECT` support;
- authenticated SOCKS5 proxy;
- one protocol per managed proxy, with generated connection URLs;
- create, disable, move, and edit proxies without restarting containers;
- encrypted storage and availability checks for third-party HTTP/SOCKS5 proxies;
- one-time enrollment tokens for secure agent registration;
- node heartbeat, uptime, traffic, active connections, RAM, goroutines, and load average;
- activity and traffic analytics across the fleet;
- infrastructure event log;
- Telegram notifications with optional HTTP(S) proxy support;
- installable PWA with per-device Web Push preferences;
- configurable system name, logo, and owner password;
- English and Russian interface;
- responsive desktop and mobile navigation;
- loopback, private, and link-local destination blocking by default;
- multi-architecture images for `linux/amd64` and `linux/arm64`.

## 🏗️ Architecture

```text
Browser
   │ HTTPS
   ▼
Reverse proxy (nginx / Caddy)
   │ 127.0.0.1:8088
   ▼
ProxyFleet control plane
   │ outbound HTTPS connections from agents
   ├───────────────┬───────────────┐
   ▼               ▼               ▼
Agent / VPS 1   Agent / VPS 2   Agent / VPS N
HTTP / SOCKS5   HTTP / SOCKS5   HTTP / SOCKS5
```

The control plane stores desired state. Agents initiate the connection to the panel, receive their configuration, and send heartbeat data back. Proxy nodes do not need a publicly exposed management port.

## 📦 Requirements

For a standard deployment:

- a Linux server;
- Docker Engine 24 or newer;
- Docker Compose v2;
- a domain and HTTPS reverse proxy for a public installation.

For local development without Docker:

- Go 1.23 or newer;
- Node.js only for JavaScript syntax checks.

## 🚀 Quick start (recommended)

Install the published image from [Docker Hub](https://hub.docker.com/r/neluckoff/proxyfleet):

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

The control plane is now available at `http://127.0.0.1:8088`.

On first open:

1. choose the owner username;
2. create a password with at least 12 characters;
3. enter the public HTTPS URL of the panel;
4. finish the setup wizard.

The password is stored only as a bcrypt hash. Session signing and data-encryption secrets are generated automatically. Persistent data lives in the `proxyfleet-control-data` Docker volume.

### Build from GitHub instead

Use this option when you want to inspect or modify the source before building:

```bash
git clone https://github.com/neluckoff/proxyfleet.git
cd proxyfleet
docker compose up -d --build
```

To use a custom registry or immutable image tag:

```bash
PROXYFLEET_IMAGE=registry.example.com/team/proxyfleet:1.0.0 \
  docker compose up -d --no-build
```

## 🌐 Production reverse proxy

The Compose service binds the panel to `127.0.0.1:8088` only. Publish it through HTTPS with nginx, Caddy, Traefik, or another reverse proxy.

Example nginx server:

```nginx
server {
    listen 443 ssl http2;
    server_name proxy.example.com;

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Secure cookies are enabled automatically for direct TLS connections or when `X-Forwarded-Proto: https` is present.

## 🛰️ Connect a node

1. Open **Nodes** in the panel.
2. Select **Add node**.
3. Enter its name, region, public host, and proxy ports.
4. Run the generated Docker command on the new VPS.

The enrollment token is valid once. After enrollment, the agent receives a permanent credential and stores it in the `proxyfleet-node-data` volume.

The generated command uses the same `neluckoff/proxyfleet:latest` image in agent mode. No additional image is required.

## 🔐 Create and manage proxies

For a managed proxy, choose:

- a display name;
- HTTP or SOCKS5;
- the node that should serve it;
- a username;
- a password, or let ProxyFleet generate one.

The complete connection URL is shown only after creation or password rotation. Save it immediately: the control plane stores only the password hash for managed proxies.

### Third-party proxies

In the same creation dialog, select **External proxy** and paste a connection URL:

```text
http://login:password@proxy.example.com:3128
socks5://login:password@proxy.example.com:1080
```

The URL is encrypted before it reaches `state.json`; passwords stay masked in the API and interface. ProxyFleet can check reachability, record latency, edit metadata, and exclude the connection from local use. It cannot disable the subscription at the external provider.

## 🔥 Firewall

Expose proxy ports only to trusted clients:

```bash
sudo ufw allow from <CLIENT_IP> to any port 3128 proto tcp
sudo ufw allow from <CLIENT_IP> to any port 1080 proto tcp
```

Do not expose proxy ports to the whole internet, even when authentication is enabled.

## 🔔 Notifications and PWA

### Telegram

The **Settings** page accepts a bot token, chat ID, and optional HTTP(S) proxy URL. The connection test calls `getMe` and sends a test message. The saved token is never returned to the browser.

Notifications cover node enrollment, offline/online transitions, configuration changes, and proxy operations.

### Web Push

ProxyFleet can be installed as a PWA. Under **Settings → Application & Push**, each browser or device can subscribe independently and choose notifications for nodes, proxies, and system events.

Production Web Push requires HTTPS. `localhost` and `127.0.0.1` remain valid for local development. VAPID keys are generated automatically and stored in the private control-plane volume.

## ♻️ Updating

### Docker Hub installation

From the directory containing `docker-compose.yml`:

```bash
cd /opt/proxyfleet
docker compose pull
docker compose up -d --no-build --remove-orphans
docker image prune -f
```

This recreates the container with the newest `neluckoff/proxyfleet:latest` image while preserving `proxyfleet-control-data`.

For reproducible production deployments, set `PROXYFLEET_IMAGE` to a `sha-<commit>` tag listed on [Docker Hub Tags](https://hub.docker.com/r/neluckoff/proxyfleet/tags).

### GitHub source installation

```bash
cd /opt/proxyfleet
git pull --ff-only
docker compose up -d --build
docker image prune -f
```

## 💾 Backup and restore

All control-plane data is stored in `proxyfleet-control-data`.

Create a backup:

```bash
docker run --rm \
  -v proxyfleet-control-data:/data:ro \
  -v "$PWD":/backup \
  alpine tar -czf /backup/proxyfleet-backup.tar.gz -C /data .
```

Restore only while the control plane is stopped:

```bash
docker compose down
docker run --rm \
  -v proxyfleet-control-data:/data \
  -v "$PWD":/backup:ro \
  alpine sh -c 'rm -rf /data/* && tar -xzf /backup/proxyfleet-backup.tar.gz -C /data'
docker compose up -d --no-build
```

## ⚙️ Configuration

| Variable | Component | Default | Purpose |
|---|---|---:|---|
| `LISTEN_ADDR` | control plane | `:8080` | HTTP listener inside the container |
| `DATA_PATH` | control plane | `/data/state.json` | Persistent state file |
| `NODE_IMAGE` | control plane | `neluckoff/proxyfleet:latest` | Image used in generated node commands |
| `PROXYFLEET_IMAGE` | Compose | `neluckoff/proxyfleet:latest` | Control-plane and agent image override |
| `CONTROL_URL` | agent | required | Public control-plane URL |
| `ENROLL_TOKEN` | agent | first start | One-time enrollment token |
| `AGENT_STATE_PATH` | agent | `/data/agent.json` | Persistent agent state |
| `HTTP_PROXY_PORT` | node Compose | `3128` | Public HTTP proxy port |
| `SOCKS_PROXY_PORT` | node Compose | `1080` | Public SOCKS5 proxy port |

## 🧑‍💻 Local development

```bash
git clone https://github.com/neluckoff/proxyfleet.git
cd proxyfleet
go mod download
DATA_PATH=./state.json LISTEN_ADDR=127.0.0.1:8080 \
  go run ./cmd/proxyfleet server
```

Run the checks:

```bash
go test ./...
go vet ./...
node --check internal/control/web/app.js
node --check internal/control/web/i18n.js
```

### Project layout

```text
cmd/proxyfleet/             server and agent entry point
internal/agent/             proxy engine, node sync, and metrics
internal/control/           HTTP API, authentication, and file storage
internal/control/web/       embedded web interface and PWA assets
internal/model/             shared control-plane and agent models
.github/workflows/          CI and Docker publishing
docker-compose.yml          control plane
docker-compose.node.yml     standalone proxy node
```

## 🔌 HTTP API

Main route groups:

- `/api/setup/*` — first-run setup;
- `/api/login`, `/api/logout`, `/api/me` — owner session;
- `/api/nodes` — node management;
- `/api/proxies` — managed and external proxy management;
- `/api/pwa/*` — Web Push subscriptions and preferences;
- `/api/analytics`, `/api/activity`, `/api/events` — observability;
- `/api/settings/*` — branding, Telegram, and password changes;
- `/api/agent/*` — enrollment, configuration, and heartbeat.

All administrative routes except setup, login, and public branding require an authenticated owner session.

## 🐳 Image publishing

`.github/workflows/docker-publish.yml` publishes `linux/amd64` and `linux/arm64` images to Docker Hub.

Required GitHub Actions secrets:

- `DOCKERHUB_USERNAME` — Docker Hub username;
- `DOCKERHUB_TOKEN` — access token with Read & Write permission.

Version tags publish semantic aliases:

```bash
git tag v0.2.0
git push origin v0.2.0
```

This produces `:0.2.0`, `:0.2`, `:0`, and `:latest`. Manual workflow runs publish `:latest` and `:sha-<commit>`.

## 🤝 Contributing

Please read [CONTRIBUTING.md](.github/CONTRIBUTING.md) before opening a pull request. Use GitHub Issues for bugs and feature requests. Report security issues privately as described in [SECURITY.md](.github/SECURITY.md).

## 🗺️ Roadmap

- mTLS or WireGuard between control plane and nodes;
- node restart and update operations from the panel;
- automatic failover and a unified gateway endpoint;
- traffic and connection limits;
- additional roles and 2FA;
- PostgreSQL support for redundant control-plane deployments.

## 📄 License

A license has not been selected yet. Until a `LICENSE` file is added, the source is available for inspection, but standard open-source usage and redistribution rights are not granted.
