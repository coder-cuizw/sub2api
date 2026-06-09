# Sub2API 生产部署指南 / Production Deployment Guide

## 快速开始 / Quick Start

### 最简单的方式 - One-Command Deploy

```bash
cd deploy
./deploy-production.sh
```

这个命令会：
1. ✅ 自动生成所有密码和密钥
2. ✅ 创建 `.env` 配置文件
3. ✅ 启动 Docker Compose（PostgreSQL + Redis + Sub2API）
4. ✅ 等待所有服务就绪
5. ✅ 显示访问链接和登录凭证

**完成后直接访问**: http://localhost:8080

## 详细部署步骤 / Detailed Steps

### 1. 前置要求 / Prerequisites

```bash
# 检查 Docker 安装
docker --version

# 检查 Docker Compose 安装
docker-compose --version  # 或 docker compose version
```

### 2. 运行部署脚本 / Run Deployment Script

```bash
# 进入部署目录
cd deploy

# 使脚本可执行（首次运行）
chmod +x deploy-production.sh

# 一键启动
./deploy-production.sh
```

### 3. 脚本会自动执行以下操作

#### a. 检查依赖
- Docker
- Docker Compose
- docker-compose.yml 文件

#### b. 生成安全凭证
```
PostgreSQL 密码:  [32 字符随机密钥]
Redis 密码:       [32 字符随机密钥]
管理员密码:       [16 字符随机密钥]
JWT 密钥:         [32 字节 hex 密钥]
TOTP 加密密钥:    [32 字节 hex 密钥]
```

#### c. 创建 .env 文件
```
.env (新生成)
├── POSTGRES_PASSWORD=[自动生成]
├── REDIS_PASSWORD=[自动生成]
├── ADMIN_PASSWORD=[自动生成]
├── JWT_SECRET=[自动生成]
├── TOTP_ENCRYPTION_KEY=[自动生成]
└── ... 其他配置项
```

#### d. 启动 Docker 容器
```bash
docker-compose up -d
```

三个容器会按顺序启动：
- `sub2api-postgres` - PostgreSQL 数据库
- `sub2api-redis` - Redis 缓存
- `sub2api` - Sub2API 应用

#### e. 等待服务就绪
脚本会自动轮询 health 检查端点，直到应用就绪。

### 4. 访问应用 / Access Application

```
URL:  http://localhost:8080
邮箱: admin@sub2api.local
密码: [显示在脚本输出中]
```

## 配置详解 / Configuration Details

### 必需配置项 / Required

| 变量 | 说明 | 自动生成 |
|------|------|---------|
| `POSTGRES_PASSWORD` | PostgreSQL 管理员密码 | ✅ 是 |
| `REDIS_PASSWORD` | Redis 密码 | ✅ 是 |
| `ADMIN_PASSWORD` | 初始管理员账户密码 | ✅ 是 |
| `JWT_SECRET` | JWT 签名密钥（需要固定，避免重启失效） | ✅ 是 |
| `TOTP_ENCRYPTION_KEY` | TOTP 加密密钥（需要固定，避免 2FA 失效） | ✅ 是 |

### 常见可选配置 / Common Optional

```bash
# 服务器配置
BIND_HOST=0.0.0.0              # 绑定地址
SERVER_PORT=8080                # 服务端口
TZ=Asia/Shanghai                # 时区

# OAuth 配置（可选）
GEMINI_OAUTH_CLIENT_ID=...
GEMINI_OAUTH_CLIENT_SECRET=...
ANTIGRAVITY_OAUTH_CLIENT_SECRET=...

# 安全配置（可选）
SECURITY_URL_ALLOWLIST_ENABLED=false
SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS=false

# 性能调优（可选）
DATABASE_MAX_OPEN_CONNS=50
REDIS_POOL_SIZE=1024
```

## 常用命令 / Common Commands

### 查看状态
```bash
docker-compose ps
```

### 查看日志
```bash
# 查看所有服务日志
docker-compose logs -f

# 只查看 Sub2API 日志
docker-compose logs -f sub2api

# 查看最后 100 行
docker-compose logs -f --tail=100 sub2api
```

### 重启服务
```bash
# 重启单个服务
docker-compose restart sub2api

# 重启所有服务
docker-compose restart
```

### 停止服务
```bash
# 停止容器但保留卷（数据不丢失）
docker-compose down

# 停止并删除卷（完全删除）
docker-compose down -v
```

### 查看数据库
```bash
# 进入 PostgreSQL 容器
docker-compose exec postgres psql -U sub2api -d sub2api

# 列表示例
\dt          # 列表所有表
\q           # 退出
```

### 进入应用容器
```bash
docker-compose exec sub2api /bin/sh
```

## 升级和更新 / Upgrade

### 更新镜像版本

编辑 `docker-compose.yml`，修改 `sub2api` 服务的 `image`：

```yaml
sub2api:
  image: 46.38.157.108:80/sub2api:v0.1.135-{commit}  # 改为新版本
```

然后重启：

```bash
docker-compose down
docker-compose up -d
```

### 保留数据升级

确保使用了 `docker-compose.yml` 中定义的卷：
```bash
# 查看卷
docker volume ls | grep sub2api

# 卷会自动保留数据
docker-compose down
# ... 更新配置 ...
docker-compose up -d
```

## 备份和恢复 / Backup & Restore

### 备份数据库

```bash
# 备份 PostgreSQL
docker-compose exec postgres pg_dump \
  -U sub2api -d sub2api \
  > backup_$(date +%Y%m%d_%H%M%S).sql

# 或使用 sub2api 自带的备份工具（如有）
```

### 备份完整配置和数据

```bash
# 备份所有内容
tar -czf sub2api_backup_$(date +%Y%m%d).tar.gz \
  .env \
  docker-compose.yml \
  volumes/

# 或备份特定卷
docker run --rm -v sub2api_postgres_data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/postgres_backup.tar.gz /data
```

## 性能优化 / Performance Tuning

### 数据库连接池
```bash
DATABASE_MAX_OPEN_CONNS=50
DATABASE_MAX_IDLE_CONNS=10
DATABASE_CONN_MAX_LIFETIME_MINUTES=30
```

### Redis 连接池
```bash
REDIS_POOL_SIZE=1024
REDIS_MIN_IDLE_CONNS=10
```

### HTTP/2 配置
```bash
GATEWAY_OPENAI_HTTP2_ENABLED=true
GATEWAY_OPENAI_HTTP2_ALLOW_PROXY_FALLBACK_TO_HTTP1=true
```

## 安全最佳实践 / Security Best Practices

### 1. 密钥管理

✅ **做这些：**
- 把 `.env` 文件保存在安全的地方
- 使用密钥管理服务（如 HashiCorp Vault）存储敏感信息
- 定期轮换密码（改 .env 文件后重启）
- 备份 `.env` 到加密存储

❌ **不要做这些：**
- 不要提交 `.env` 到 Git
- 不要在日志中输出密码
- 不要使用简单密码
- 不要共享 `.env` 文件内容

### 2. 网络安全

```bash
# 只绑定到本地（通过 Nginx 反向代理访问）
BIND_HOST=127.0.0.1

# 或使用 iptables 限制访问
iptables -A INPUT -p tcp --dport 8080 -j REJECT
```

### 3. 数据库安全

```bash
# 使用强密码（已自动生成）
POSTGRES_PASSWORD=...  # 32 字符随机

# 定期备份
0 2 * * * docker-compose exec postgres pg_dump ...
```

## 故障排查 / Troubleshooting

### 容器无法启动

```bash
# 查看详细错误日志
docker-compose logs -f sub2api

# 检查 .env 文件是否存在
ls -la .env

# 检查环境变量
docker-compose config | grep -A 20 "environment:"
```

### 数据库连接失败

```bash
# 检查 PostgreSQL 状态
docker-compose ps

# 测试数据库连接
docker-compose exec postgres \
  psql -U sub2api -d sub2api -c "SELECT 1"
```

### Redis 连接失败

```bash
# 检查 Redis 日志
docker-compose logs redis

# 测试 Redis 连接
docker-compose exec redis redis-cli ping
```

### 端口被占用

```bash
# 检查占用端口的进程
lsof -i :8080

# 修改 .env 中的 SERVER_PORT
SERVER_PORT=8081
docker-compose down && docker-compose up -d
```

## 文件结构 / Directory Structure

```
deploy/
├── docker-compose.yml                # 完整配置（推荐）
├── docker-compose.standalone.yml     # 仅应用（使用外部数据库）
├── docker-compose.dev.yml            # 开发配置
├── docker-compose.local.yml          # 本地配置
├── deploy-production.sh              # 一键部署脚本 ← 使用这个
├── .env                              # 自动生成的配置（不提交到 Git）
└── README.md                         # 部署说明

volumes/
├── postgres_data/                    # PostgreSQL 数据
├── redis_data/                       # Redis 数据
└── sub2api_data/                     # 应用数据
```

## 相关文档 / Related Documentation

- [DOCKER_DEPLOYMENT.md](./DOCKER_DEPLOYMENT.md) - Docker 镜像构建指南
- [ANTI_BLOCKADE_GUIDE.md](./ANTI_BLOCKADE_GUIDE.md) - 反封禁功能指南
- [TRAFFIC_MODULATION_INTEGRATION.md](./TRAFFIC_MODULATION_INTEGRATION.md) - 流量调制功能指南
- [deploy/README.md](./deploy/README.md) - 部署详细说明

## 获取帮助 / Getting Help

如遇到问题，请查看：
1. [故障排查部分](#故障排查--troubleshooting)
2. `docker-compose logs` 输出
3. 项目 GitHub Issues
4. 应用内的帮助文档

## 更新日志 / Changelog

### v1.0.0 (2026-06-09)
- ✨ 首次发布生产级部署脚本
- ✨ 自动密码和密钥生成
- ✨ 完整的 Docker Compose 配置
- 📝 详细的部署和维护文档
