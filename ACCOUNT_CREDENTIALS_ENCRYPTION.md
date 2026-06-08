# 账号上游凭证静态加密（Account Credentials Encryption at Rest）

## 这是什么 / 不是什么

**是**：把账号上游凭证（`access_token` / `refresh_token` / `id_token` / `api_key` /
`session_key` / `cookie` / `aws_secret_access_key` / `aws_session_token` /
`service_account_json` / `service_account` / `private_key`）在写入数据库前用
**AES-256-GCM** 加密，复用项目已有的 `totp.encryption_key`。目的是：**当数据库或备份
泄漏时，这些凭证不是明文。**

**不是**：它与"防封 / 反检测"**毫无关系**——不会改变上游（Anthropic 等）看到的任何内容，
不影响账号是否被风控。它纯粹是你这一侧的安全卫生。

默认 **关闭**。不开启时，所有读写行为与现状完全一致。

---

## 前置条件（务必先满足）

1. **配置一个持久、固定的 `totp.encryption_key`**（32 字节、64 位 hex）。
   ```yaml
   totp:
     encryption_key: "<64 hex chars>"   # 例如 openssl rand -hex 32
   ```
   - 若该项为空，系统会**自动生成一个临时密钥（重启即变）**。此时即使打开加密开关，
     程序也会**拒绝写加密并告警**（`account_credentials_encryption_ignored`），
     以免用临时密钥加密后重启无法解密。
2. **备份这把密钥，并视同最高机密永久保管。**
   一旦有凭证被它加密，**丢失或更换这把密钥 = 这些凭证永久不可恢复**。

---

## 启用步骤

1. 确认 `totp.encryption_key` 已配置为固定值（见上）。
2. 打开开关：
   ```yaml
   security:
     encrypt_account_credentials: true
   ```
3. 重启服务。启动日志应出现：
   ```
   INFO account_credentials_cipher_configured encrypt_on_write=true
   ```
   若看到 `account_credentials_encryption_ignored`，说明密钥未持久配置，写加密被忽略
   （读仍向后兼容），请回到前置条件第 1 步。

### 加密是惰性的

开启后**只有新写入/更新的凭证会被加密**（如重新授权、token 刷新、编辑账号）。
**存量明文行不会自动加密**，但能正常读取。如需立刻把存量也加密，可在低峰期触发一次
全量"无害更新"（例如对每个账号做一次不改值的保存）让其重写落库。目前**没有内置回填
脚本**——如需要我可以另写一条一次性 migration/CLI。

---

## 行为与安全保证

- **读向后兼容**：只有带 `enc:v1:` 前缀的值才会被解密；明文值、历史值原样返回。
  因此明文与密文可在同一行、同一字段历史中共存。
- **解密失败安全**：若某密文解不开（通常是换错了密钥），该字段会被**丢弃并告警**
  （`account_credentials_decrypt_failed`），**绝不会把密文当作 token 发往上游**。
  现象是该账号鉴权失败——这是正确的失败方式，提示你密钥配置有误。
- **键级、保持结构**：只加密敏感的字符串字段，不改 JSONB 结构，兼容
  `credentials || $jsonb` 的部分合并更新。
- **API / 日志**：API 响应本就通过 `RedactCredentials` 抹掉敏感字段（只回
  `has_<key>` 存在性），与本加密相互独立、叠加生效。

---

## 回滚

- 把 `security.encrypt_account_credentials` 改回 `false` 并重启即可。
  **新写入恢复明文，但已加密的存量行仍会被正常解密**——因为只要
  `totp.encryption_key` 不变，读路径始终带着解密能力。
- ⚠️ **回滚 ≠ 可以删除密钥**。只要库里还存在任何 `enc:v1:` 密文，
  `totp.encryption_key` 就必须保持不变。要彻底移除加密，需先把所有账号重写为明文
  （触发一次全量更新且开关为 false），确认无 `enc:v1:` 残留后，方可考虑动密钥。

---

## 上线前验证清单（强烈建议在 staging 先跑）

凭证读取在**每个网关请求的热路径**上，启用前请在 staging（含真实 Postgres + Redis）验证：

- [ ] 配置固定 `totp.encryption_key` + `encrypt_account_credentials: true`，启动日志为 `encrypt_on_write=true`。
- [ ] 新建 / 重新授权一个各类型账号（Claude OAuth、API key、Vertex service account…），
      直接查库确认敏感字段已是 `enc:v1:` 前缀。
- [ ] 用该账号实际跑通一次上游请求（鉴权、token 刷新、调度命中均正常）。
- [ ] 存量明文账号在开启加密后仍能正常请求（读向后兼容）。
- [ ] 重启服务后上述账号仍可用（验证密钥持久、无临时密钥陷阱）。
- [ ] 跑账号相关集成测试套件（`-tags=integration`）。
- [ ] 演练回滚：开关改回 false 后，已加密账号仍可正常解密使用。

验证通过后再灰度到生产。

---

## 相关代码

- 加密组件：`backend/internal/repository/credential_cipher.go`
- 接线：`backend/internal/repository/account_repo.go`（`NewAccountRepository` 配置开关；
  读 funnel `accountEntityToService` 解密；4 处写路径加密）
- 配置项：`backend/internal/config/config.go` → `SecurityConfig.EncryptAccountCredentials`
- 复用的加密器：`backend/internal/repository/aes_encryptor.go`（AES-256-GCM）
