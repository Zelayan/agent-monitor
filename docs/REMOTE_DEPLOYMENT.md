# 远端 IP 部署与 PWA / HTTPS 指南 (Remote Deployment & PWA Guide)

本文档面向需要将 **AGENT MONITOR** 部署到局域网远程主机（如 `192.168.x.x`）或私有云服务器，并希望在客户端浏览器（Chrome / Edge / Safari）中启用 **PWA 独立桌面 App 安装** 与 **Service Worker** 的开发者。

---

## 目录 (Table of Contents)

1. [背景：为什么跨 IP 访问需要安全上下文？](#1-背景为什么跨-ip-访问需要安全上下文)
2. [方案一：客户端 Chrome 启用信任白名单（最快，免证书）](#2-方案一客户端-chrome-启用信任白名单最快免证书)
3. [方案二：Caddy 自动签发局域网证书（推荐，全自动）](#3-方案二caddy-自动签发局域网证书推荐全自动)
4. [方案三：Nginx 自签名证书反向代理](#4-方案三nginx-自签名证书反向代理)
5. [各 Agent 如何上报到远端 Monitor 服务](#5-各-agent-如何上报到远端-monitor-服务)

---

## 1. 背景：为什么跨 IP 访问需要安全上下文？

根据 W3C 安全上下文规范（Secure Contexts）：
- 当通过本地地址（`http://127.0.0.1:8000` 或 `http://localhost:8000`）访问时，所有现代浏览器**默认视其为安全上下文**，可**直接启用 PWA 与 Service Worker，无需任何 SSL 证书**。
- 当通过**远端 IP**（如 `http://192.168.1.100:8000` 或域名）跨网络访问时，浏览器为了防范中间人劫持，**强制要求 `https://` 协议**才会激活 PWA 安装提示和 Service Worker 离线缓存。

---

## 2. 方案一：客户端 Chrome 启用信任白名单（最快，免证书）

如果你只是在个人内网/工作站之间调试使用，**无需折腾任何证书与反向代理**，直接利用 Chromium 内核内置的开发者白名单功能：

1. 在你的客户端电脑上打开 Chrome 或 Edge，在地址栏输入并回车：
   ```text
   chrome://flags/#unsafely-treat-insecure-origin-as-secure
   ```
2. 将该选项状态切换为 **Enabled**；
3. 在下方出现的文本框中填入你远端主机的完整 IP 与端口（多个地址用逗号隔开），例如：
   ```text
   http://192.168.1.100:8000
   ```
4. 点击页面右下角的 **Relaunch** 重启浏览器。

> **效果**：浏览器会将此远端 IP 视为本地安全源。再次访问 `http://192.168.1.100:8000` 时，地址栏右侧将正常出现 **「安装 AGENT MONITOR」** 图标，支持一键安装为无边框独立桌面 App！

---

## 3. 方案二：Caddy 自动签发局域网证书（推荐，全自动）

如果局域网内有多人需要访问，推荐在远端主机上部署极简反向代理 [Caddy](https://caddyserver.com/)。Caddy 内置本地 CA，可以自动为局域网 IP 签发合法 TLS 证书。

### 1. 安装 Caddy
```bash
# Ubuntu / Debian
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy

# macOS
brew install caddy
```

### 2. 配置文件 (`/etc/caddy/Caddyfile` 或当前目录 `Caddyfile`)
假设远端服务器 IP 为 `192.168.1.100`：
```Caddyfile
192.168.1.100 {
    tls internal
    reverse_proxy 127.0.0.1:8000
}
```

### 3. 启动并访问
```bash
caddy run
```
客户端电脑直接访问 `https://192.168.1.100` 即可解锁原生 HTTPS 和 PWA 桌面安装。

---

## 4. 方案三：Nginx 自签名证书反向代理

在已有 Nginx 的 Linux 服务器上，生成自签名证书并配置反向代理：

### 1. 生成自签名 SSL 证书
```bash
sudo mkdir -p /etc/nginx/ssl
sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/nginx/ssl/agent-monitor.key \
  -out /etc/nginx/ssl/agent-monitor.crt \
  -subj "/CN=192.168.1.100"
```

### 2. 配置 Nginx 站点
编辑 `/etc/nginx/conf.d/agent-monitor.conf`：
```nginx
server {
    listen 443 ssl http2;
    server_name 192.168.1.100;

    ssl_certificate /etc/nginx/ssl/agent-monitor.crt;
    ssl_certificate_key /etc/nginx/ssl/agent-monitor.key;

    # 支持 SSE 实时流广播与普通 API 转发
    location / {
        proxy_pass http://127.0.0.1:8000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 保持 SSE 实时长连接
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding on;
    }
}
```

### 3. 重载 Nginx
```bash
sudo nginx -t && sudo systemctl reload nginx
```
通过 `https://192.168.1.100` 访问（首次需要在浏览器点击“高级 -> 继续访问”一次）。

---

## 5. 各 Agent 如何上报到远端 Monitor 服务

当 Monitor 仪表盘部署在远端主机时，开发机本地运行的 `agent-reporter` 需要将事件定向到远端服务地址。

可以通过环境变量 `AGENT_MONITOR_URL` 指向远端服务器：

```bash
# 写入开发机的终端配置文件 (~/.bashrc 或 ~/.zshrc)
export AGENT_MONITOR_URL="http://192.168.1.100:8000"
# 或如果是 HTTPS 反代：
# export AGENT_MONITOR_URL="https://192.168.1.100"
```

测试联通性：
```bash
curl -X POST "$AGENT_MONITOR_URL/api/event" \
  -H "Content-Type: application/json" \
  -d '{"id":"ping-test","event":"SessionStart","agent":"Ping","title":"Remote Connectivity Test"}'
```
远端仪表盘若实时显示 `Remote Connectivity Test` 任务卡片，即表示远端协同监控已成功建立！
