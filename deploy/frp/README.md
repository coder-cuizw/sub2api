# Sub2API 本地部署 + frp 内网穿透

把本地运行的 Sub2API（默认 `8080`）通过 [frp](https://github.com/fatedier/frp) 穿透到公网 frps，
**远程端口与本地端口保持一致（8080）**。

## 1. 本地部署 Sub2API

使用预构建镜像（推荐，最快）：

```bash
cd deploy
cp .env.example .env          # 按需修改；至少设置 POSTGRES_PASSWORD / JWT_SECRET / TOTP_ENCRYPTION_KEY / ADMIN_PASSWORD
mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
curl http://127.0.0.1:8080/health        # {"status":"ok"}
```

> 从当前源码构建：`docker compose -f docker-compose.dev.yml up -d --build`
> （构建阶段需要访问 Alpine / Go / npm 源，在做 TLS 审查的网络里会失败，此时请用预构建镜像）。

生成密钥：

```bash
openssl rand -hex 32   # JWT_SECRET / TOTP_ENCRYPTION_KEY
```

## 2. 安装 frpc

```bash
# Linux amd64 示例
VER=v0.69.0
curl -sSL -O https://github.com/fatedier/frp/releases/download/${VER}/frp_${VER#v}_linux_amd64.tar.gz
tar xzf frp_${VER#v}_linux_amd64.tar.gz
sudo cp frp_${VER#v}_linux_amd64/frpc /usr/local/bin/frpc
```

## 3. 配置并启动隧道

```bash
cd deploy/frp
cp frpc.toml.example frpc.toml
# 在 frpc.toml 填入真实 auth.token，然后：
frpc -c frpc.toml
```

`frpc.toml` 关键内容（本地端口 = 远程端口）：

```toml
serverAddr = "46.38.157.108"
serverPort = 443
auth.method = "token"
auth.token  = "<frps token>"

[[proxies]]
name       = "sub2api-http"
type       = "tcp"
localIP    = "127.0.0.1"
localPort  = 8080
remotePort = 8080
```

启动成功后，公网访问：`http://46.38.157.108:8080`

对应的 frps 服务端配置（参考）：

```toml
bindAddr = "0.0.0.0"
bindPort = 443
auth.method = "token"
auth.token  = "<frps token>"
webServer.addr     = "0.0.0.0"
webServer.port     = 7500
webServer.user     = "admin"
webServer.password = "<dashboard password>"
```

## 安全说明

- `frpc.toml`（含真实 token）已在 `.gitignore` 中忽略，请勿提交。仓库只保留 `frpc.toml.example`。
- frps 的 `bindPort = 443` 必须能从客户端直接以**原始 TCP** 访问。若客户端所在网络做了
  TLS 审查 / 只放行被检测的 HTTP(S)（例如某些云沙箱出网代理），frp 的隧道协议无法穿过，
  会出现 `connect to server error: EOF`。这种情况下请在普通服务器/本机上运行 frpc。
