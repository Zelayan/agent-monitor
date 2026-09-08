# Remote Deployment, HTTPS & Security Guide (REMOTE_DEPLOYMENT.en.md)

[English](REMOTE_DEPLOYMENT.en.md) | [简体中文](REMOTE_DEPLOYMENT.md)

This document guides developers deploying **AGENT MONITOR** to a LAN remote server (e.g. `192.168.x.x`), private cloud, or public VPS, who want to enable **PWA standalone desktop installation**, **Service Worker offline capabilities**, and **cryptographic API Key authentication**.

---

## Table of Contents

1. [Background: Secure Contexts for Cross-Network Access](#1-background-secure-contexts-for-cross-network-access)
2. [Option 1: Chrome Insecure Origin Whitelist (Fastest, No TLS Certs)](#2-option-1-chrome-insecure-origin-whitelist-fastest-no-tls-certs)
3. [Option 2: Caddy Reverse Proxy with Automatic LAN Certificates (Recommended)](#3-option-2-caddy-reverse-proxy-with-automatic-lan-certificates-recommended)
4. [Option 3: Nginx Reverse Proxy with Self-Signed Certificates](#4-option-3-nginx-reverse-proxy-with-self-signed-certificates)
5. [Reporting Agent Events to a Remote Monitor Service](#5-reporting-agent-events-to-a-remote-monitor-service)
6. [Remote Security: Multi-Tenant & API Key Access Control](#6-remote-security-multi-tenant--api-key-access-control)

---

## 1. Background: Secure Contexts for Cross-Network Access

According to W3C Secure Contexts specifications:
- Accessing local addresses (`http://127.0.0.1:8000` or `http://localhost:8000`) is **considered a secure context by default** across all modern browsers. PWA desktop installation and Service Worker caching activate out of the box with zero SSL setup.
- Accessing remote IP addresses (such as `http://192.168.1.100:8000` or public hostnames) over the network requires the `https://` protocol to prevent man-in-the-middle attacks before browser PWA prompts and offline caches are activated.

---

## 2. Option 1: Chrome Insecure Origin Whitelist (Fastest, No TLS Certs)

For personal development across workstations on a trusted local network, you can bypass certificate setup using Chromium's built-in flag:

1. In Chrome or Edge on your client machine, navigate to:
   ```text
   chrome://flags/#unsafely-treat-insecure-origin-as-secure
   ```
2. Switch the flag state to **Enabled**;
3. In the text area, enter the remote server IP and port (comma-separated for multiple origins), e.g.:
   ```text
   http://192.168.1.100:8000
   ```
4. Click **Relaunch** at the bottom right.

> **Result**: The browser treats this remote origin as secure. Navigating to `http://192.168.1.100:8000` displays the **Install AGENT MONITOR** button in the address bar.

---

## 3. Option 2: Caddy Reverse Proxy with Automatic LAN Certificates (Recommended)

When multiple team members access the dashboard, [Caddy](https://caddyserver.com/) provides zero-configuration local TLS with its internal certificate authority.

### 1. Install Caddy
```bash
# Ubuntu / Debian
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy

# macOS
brew install caddy
```

### 2. Configuration (`/etc/caddy/Caddyfile` or `./Caddyfile`)
Assuming the remote server IP is `192.168.1.100`:
```Caddyfile
192.168.1.100 {
    tls internal
    reverse_proxy 127.0.0.1:8000
}
```

### 3. Run and Access
```bash
caddy run
```
Navigate to `https://192.168.1.100` in the client browser to access native HTTPS and PWA install prompts.

---

## 4. Option 3: Nginx Reverse Proxy with Self-Signed Certificates

On Linux servers already running Nginx:

### 1. Generate Self-Signed TLS Certificate
```bash
sudo mkdir -p /etc/nginx/ssl
sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/nginx/ssl/agent-monitor.key \
  -out /etc/nginx/ssl/agent-monitor.crt \
  -subj "/CN=192.168.1.100"
```

### 2. Configure Nginx Server Block
Edit `/etc/nginx/conf.d/agent-monitor.conf`:
```nginx
server {
    listen 443 ssl http2;
    server_name 192.168.1.100;

    ssl_certificate /etc/nginx/ssl/agent-monitor.crt;
    ssl_certificate_key /etc/nginx/ssl/agent-monitor.key;

    location / {
        proxy_pass http://127.0.0.1:8000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Critical: Support SSE live streaming without buffering
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;
    }
}
```

### 3. Reload Nginx
```bash
sudo nginx -t && sudo systemctl reload nginx
```
Access via `https://192.168.1.100` (accept the self-signed warning on first visit).

---

## 5. Reporting Agent Events to a Remote Monitor Service

When Monitor runs on a remote server, local development machines dispatch events via `agent-reporter` by pointing to the remote service address:

```bash
# In shell profile (~/.bashrc or ~/.zshrc)
export AGENT_MONITOR_URL="http://192.168.1.100:8000"
# Or with HTTPS:
# export AGENT_MONITOR_URL="https://192.168.1.100"
```

---

## 6. Remote Security: Multi-Tenant & API Key Access Control

When exposing Agent Monitor to a public VPS or shared team network, configure **API Key Access Control** to prevent unauthorized access or accidental deletion:

### 1. Server-Side Authentication Options
Launch `agent-monitor` with environment variables or CLI flags:
```bash
# Pattern A: Single Shared Secret
export AGENT_MONITOR_API_KEY="your-strong-secret-token"
agent-monitor -port 8000

# Pattern B: Multi-Project Namespace Isolation + Master Key (Recommended for teams)
# Format: projName=secretKey, comma-separated
export AGENT_MONITOR_API_KEYS="projA=token_alpha,projB=token_beta"
export AGENT_MONITOR_MASTER_KEY="master-super-secret-key"
agent-monitor -port 8000
```

### 2. Client Reporters Configured by Project
Within each local project repository:
```bash
# Under Project A directory:
agent-reporter init-config --local --url "https://monitor.domain.com/api/event" --api-key "token_alpha"

# Under Project B directory:
agent-reporter init-config --local --url "https://monitor.domain.com/api/event" --api-key "token_beta"
```
Sessions created by reporters automatically route into their designated project workspaces.

### 3. Web Dashboard Authentication & Switching
When opening the dashboard in a browser:
- Entering `token_alpha`: Shows only Project A sessions; SSE live streams isolate to Project A; mutations apply only to Project A;
- Entering `token_beta`: Isolated view for Project B;
- Entering `master-super-secret-key`: Unlocks the 👑 Master view, overseeing and managing all project workspaces across the system.

Verify connectivity:
```bash
curl -X POST "$AGENT_MONITOR_URL/api/event" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer token_alpha" \
  -d '{"id":"ping-test","event":"SessionStart","agent":"Ping","title":"Remote Connectivity Test"}'
```
When the `Remote Connectivity Test` session appears in real time on the dashboard, the remote pipeline is successfully verified.
