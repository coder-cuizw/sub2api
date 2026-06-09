# Docker 部署指南 / Docker Deployment Guide

## 快速开始 / Quick Start

### 在 Linux 服务器上构建和推送镜像 / On Your Linux Server

```bash
# 克隆项目（如果还未克隆）/ Clone the project (if not already cloned)
git clone https://github.com/coder-cuizw/sub2api.git
cd sub2api
git checkout claude/request-body-transform-7i2fjw

# 执行构建脚本 / Run the build script
./build-and-push.sh

# 或者使用自定义参数 / Or use custom parameters
./build-and-push.sh 46.38.157.108:80 sub2api
```

### 一行命令 / One-Liner

```bash
docker build -t 46.38.157.108:80/sub2api:$(cat backend/cmd/server/VERSION | tr -d '\n')-$(git rev-parse --short HEAD) \
  -t 46.38.157.108:80/sub2api:latest . && \
docker push 46.38.157.108:80/sub2api:$(cat backend/cmd/server/VERSION | tr -d '\n')-$(git rev-parse --short HEAD) && \
docker push 46.38.157.108:80/sub2api:latest
```

## 构建脚本详解 / Build Script Details

### `build-and-push.sh` 用法

```bash
# 默认参数（推荐）/ Default parameters (recommended)
./build-and-push.sh

# 自定义注册表 / Custom registry
./build-and-push.sh 192.168.1.100:5000

# 自定义注册表和镜像名称 / Custom registry and image name
./build-and-push.sh 192.168.1.100:5000 myapp

# 自定义所有参数 / Custom all parameters
./build-and-push.sh 192.168.1.100:5000 myapp v1.0.0-custom
```

### 版本号方案 / Versioning Scheme

构建脚本自动使用以下版本号方案：

The build script automatically uses the following versioning scheme:

```
v{VERSION}-{COMMIT_HASH}

示例 / Example:
v0.1.135-ff7758c
```

其中：
- `VERSION` 来自 `backend/cmd/server/VERSION` 文件
- `COMMIT_HASH` 是当前 Git 提交的短哈希

Where:
- `VERSION` comes from `backend/cmd/server/VERSION` file
- `COMMIT_HASH` is the short Git commit hash of current HEAD

### 生成的镜像标签 / Generated Image Tags

每次构建会生成两个标签：

Two tags are generated on each build:

1. **版本标签** / Version tag: `46.38.157.108:80/sub2api:v0.1.135-ff7758c`
   - 用于特定版本部署 / For specific version deployments
   - 便于版本追踪 / Enables version tracking

2. **最新标签** / Latest tag: `46.38.157.108:80/sub2api:latest`
   - 用于快速部署最新版本 / For quick deployment of latest version
   - 便于 CI/CD 自动化 / For CI/CD automation

## 镜像包含内容 / What's Included in the Image

✓ Go 后端应用（带嵌入式前端）/ Go backend application (with embedded frontend)
✓ Node.js 前端构建 / Node.js frontend build  
✓ PostgreSQL 客户端工具（pg_dump, psql）/ PostgreSQL client tools (pg_dump, psql)
✓ 完整的 anti-blockade 功能 / Complete anti-blockade features:
  - OAuth token 指纹校验 / OAuth token fingerprint validation
  - TLS 客户端校验 / TLS client validation  
  - CCH 签名支持 / CCH signing support
  - 流量调制服务 / Traffic modulation service
  - 账号冷却机制 / Account cooldown mechanism

## 部署后验证 / Post-Deployment Verification

```bash
# 检查镜像 / Check image
docker images | grep sub2api

# 运行容器 / Run container
docker run -d \
  --name sub2api \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://user:password@postgres:5432/sub2api" \
  -e REDIS_URL="redis://redis:6379/0" \
  46.38.157.108:80/sub2api:latest

# 检查健康状态 / Check health
curl http://localhost:8080/health
```

## 故障排查 / Troubleshooting

### 推送失败 / Push Fails

如果推送到私有注册表失败：

If push to private registry fails:

```bash
# 检查注册表访问 / Check registry access
curl -v http://46.38.157.108:80/v2/

# 如果需要认证，可能需要登录 / If auth is required, you might need to login
docker login 46.38.157.108:80
```

### 构建失败 / Build Fails

如果构建失败：

If build fails:

```bash
# 检查网络连接 / Check network connectivity
ping goproxy.cn
ping docker.io

# 清空 Docker 缓存并重试 / Clear Docker cache and retry
docker build --no-cache -t 46.38.157.108:80/sub2api:latest .

# 查看完整日志 / View full build logs
docker build -t 46.38.157.108:80/sub2api:latest . --progress=plain
```

## 版本更新 / Version Updates

当更新版本时：

When updating versions:

1. 更新 `backend/cmd/server/VERSION` 文件
2. 提交更改到 Git
3. 运行构建脚本

Steps:

1. Update `backend/cmd/server/VERSION` file
2. Commit changes to Git
3. Run the build script

构建脚本会自动使用新版本号创建标签。

The build script will automatically use the new version number for tagging.

## 相关文档 / Related Documentation

- [ANTI_BLOCKADE_GUIDE.md](./ANTI_BLOCKADE_GUIDE.md) - Anti-blockade 功能完整指南
- [TRAFFIC_MODULATION_INTEGRATION.md](./TRAFFIC_MODULATION_INTEGRATION.md) - 流量调制集成指南
- [deploy/README.md](./deploy/README.md) - 部署说明

