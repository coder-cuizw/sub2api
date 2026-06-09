# OAuth 订阅号防封完全指南

> **免责声明**：本指南基于逆向工程、公开信息和推理，不代表 Anthropic 的实际检测机制。所有使用均自行承担风险。
>
> **本项目用途**：为了学习和研究，帮助了解代理/中转的原理。任何违反 Claude 服务条款的使用均由用户自行承担后果。

---

## 核心认知

Anthropic 检测第三方应用的信号分三个层级，从高到低：

| 层级 | 信号 | 作用 | 可靠性 |
|------|------|------|--------|
| **Tier 1: 网络/加密** | TLS 指纹(JA3/JA4)、HTTP/2 frame 顺序、IP 信誉 | 物理层暴露，无法伪装 | 🔴 最强 |
| **Tier 2: 客户端自洽** | User-Agent / header 顺序 / system prompt 格式 / metadata 稳定性 | 行为一致性检查 | 🟠 中等 |
| **Tier 3: 逆向猜测** | CCH 签名、cc_version 指纹、billing block 格式 | 基于抓包反推，可能猜错 | 🟢 低（不要押宝） |

**最容易被封的原因**（按可能性排序）：
1. TLS 指纹暴露（握手时已经输）
2. 量级异常（一个订阅 7×24 不睡觉、多人并发）
3. IP 异常（机房 IP / 代理 IP 与通常位置不符）
4. 账号关联（一个 OAuth token 下多个不同 device_id / metadata.user_id）
5. Header 自洽性破裂（claimed User-Agent 版本与实际发送的 header 字段版本不符）

**不太可能被封的原因**：
- `cch=00000` 静态占位符（大概率无关，或最多计费归因错乱）
- `cc_version` 后缀 3 字符 hash 错误（除非 Anthropic 真的验证，但更可能是忽略）
- system prompt 缺少某个逆向猜测的字段（Anthropic 用 pattern 匹配，有则检测，无则放过）

---

## 分阶段防封指南

### 第一阶段：账号级（申请、上号、获取 token）

#### 1.1 选号
- **用新的、未被滥用的账号**。已有黑历史的 email 重新注册也会被关联。
- **避免特征聚集**：不要用同一个 email 批量注册多个订阅。每个订阅号都应该是独立的。
- **真实 IP / 真实地区**：账号申请和首次登录应该在真实 IP + 账号声称的地区。不要刚注册就从日本代理打请求。

#### 1.2 申请订阅
- **真人行为**：申请时不要 burst（一秒钟申请 10 个），间隔 > 1 小时。
- **账号预热**：订阅申请后，让它在真实 IP 上躺 1-3 天，偶尔登网页看看，建立"真人账号"的行为历史。
- **支付方式多元化**：不同账号用不同信用卡/支付渠道。同一张卡办 100 个 Claude Pro，等于自己报告"这是批量操作"。

#### 1.3 获取 OAuth token
```bash
# 通过官方 Claude Web 登录，从浏览器 DevTools → Network 里抓 Authorization header
# 或使用官方 CLI login 流程获取
# 务必用真实设备 / 真实 IP / 真实浏览器指纹
```

**关键**：OAuth token 第一次使用应该是从获取它的同一个 IP 地址、同一个地区、同一个设备。

---

### 第二阶段：代理网关级（接收请求、转换请求体、转发上游）

#### 2.1 TLS/HTTP 指纹管理（Tier 1，最关键）

这一步决定成败。即使 header 完美伪装，TLS 握手时的指纹一暴露就完蛋。

**你的项目已有的机制**：

- **TLS Fingerprint Profiles** 数据库表
  - 内置了 `Linux x64 Node.js v22.22.2` 的真实指纹
  - 支持自定义指纹模板（cipher_suites、curves、extensions 等）

**正确做法**：

```yaml
# 应用启动时，为每个 OAuth 账号绑定一个 TLS 指纹模板

# 方案 A：所有账号用同一个预置指纹（简单、安全、推荐）
oauth_accounts:
  - id: account_1
    token: "sk-..."
    tls_fingerprint_profile_id: 1  # 指向 "Node.js v22.22.2" 模板
    
# 方案 B：账号多轮转用不同指纹（复杂、高隐蔽，但容易出现 metadata 关联）
```

**风险检查清单**：
- [ ] 指纹获取失败时，是否有 fallback？（目前代码是直接降级，该改为硬报错或用内置默认指纹）
- [ ] 指纹模板的 cipher_suites 顺序和官方 CLI 完全一致吗？
- [ ] 是否在 HTTP/2 frame 的 SETTINGS 和 WINDOW_UPDATE 的顺序上也模仿了 Node.js？（细节）

**项目中的坑**：
```go
// gateway_service.go:6288-6295
fp, err := s.identityService.GetOrCreateFingerprint(ctx, account.ID, clientHeaders)
if err != nil {
    logger.LegacyPrintf(...)  // 仅打日志，继续执行
    // 后续 fingerprint=nil，所有 HTTP header 伪装都跳过了
    // 但 TLS 握手还是要做，此时已经暴露
}
```

**修复建议**：
```go
if err != nil {
    // 不要继续，直接返回错误，防止部分应用造成的混合指纹
    return nil, nil, fmt.Errorf("critical: fingerprint required for OAuth OAuth account: %w", err)
}
```

---

#### 2.2 Client Identity 管理（User-Agent / device_id / metadata）

**问题定义**：一个 OAuth token 代表一个真实用户订阅。如果一个 token 被多个人/多个设备用，Anthropic 的 device_id / user_id 关联检测会识别出来。

**你的项目里的机制**：
- `metadata.user_id` 被重写成 `"{account_uuid}:{device_id}"` 格式
- `device_id` 从 TLS 指纹生成（ClientID）
- 每次请求都对应一个固定的虚拟 device

**正确做法**：

```go
// gateway_service.go:6300-6307
// 现有逻辑：if !enableMPT（metadata passthrough 关闭）时才重写
// 问题：enableMPT=true 时直接透传客户端 user_id，如果下游改了 device_id，会被识别为多设备

// 修复后：OAuth 账号必须强制重写 metadata，不要透传
if account.IsOAuth() {  // 无条件，不受 enableMPT 影响
    body = s.identityService.RewriteUserIDWithMasking(ctx, body, account, accountUUID, fp.ClientID, ...)
}
```

**metadata 重写的三个层面**：

1. **device_id 固定化**
   ```json
   // before: metadata.user_id 可能被下游随意改写
   // after: 强制改写为 "{account_uuid}:{fixed_device_id}"
   ```
   
2. **User-Agent 与 cc_version 对齐**
   ```
   User-Agent: claude-cli/2.1.161  (伪造)
   system[0].text: "x-anthropic-billing-header: cc_version=2.1.161.{fp}; ..."
   
   这两个版本号必须一致，否则被识别为改写工具
   ```

3. **x-stainless-* header 全套伪装**
   ```
   x-stainless-helper-method: claude-cli 
   x-stainless-os: Linux
   x-stainless-arch: x64
   // 这些必须和 TLS 握手时的 User-Agent 匹配
   ```

**项目中的风险**：
```go
// openai_gateway_chat_completions.go:226
buildUpstreamRequest(..., false)  // mimicClaudeCode 硬编码为 false
// 结果：OpenAI 兼容路径不会注入 Claude CLI header
// 如果这条路径也会打 Anthropic 上游（双转），会暴露真实身份
```

---

#### 2.3 请求体自洽性

不要过度解读 billing block / CCH 这些逆向产物。重点是：

**✅ 必做（Tier 2）**：

1. **system prompt 格式**
   ```go
   // 确保：
   // ① system 是数组，不是字符串
   // ② 至少有一个 text block 包含 "You are Claude Code" 前缀
   // ③ 不要有显然的工具库 marker（如 "Sub2API", "gateway", "proxy"）
   
   // 你们的 buildBillingAttributionBlockJSON + ensureClaudeCodeSystemBlocks 
   // 做的就是这个，要确保所有 OAuth 路径都进来
   ```

2. **Beta header 一致**
   ```go
   // anthropic-beta header 和 body 里的特性声明要匹配
   // 如果 header 里声明 "interleaved-thinking-2025-05-14"
   // 但 body 里没有 thinking 字段，会被认为改写过
   ```

3. **metadata.user_id 稳定**
   ```json
   {
     "metadata": {
       "user_id": "account-uuid:device-id"  // 这个值必须稳定
     }
   }
   // 如果一个 OAuth 账号在 10 分钟内发出 10 个不同的 user_id，
   // 等于"一个订阅多个人在用"，直接关联检测
   ```

**❓ 可能有用但置信度低（Tier 3，不要押宝）**：

- `cch` 签名（xxHash64）— 目前默认关闭，原因：①代码里无条件跳过的风险②算法可能猜错③即使错也可能被忽略
- `cc_version` 后缀指纹 — 同上

---

### 第三阶段：运维级（监控、控制量级、隐蔽使用）

#### 3.1 并发 / 速率控制（Tier 1.5，最容易被抓）

一个真实的 Claude Pro 用户：
- 平均一天几十个请求，不是几千个
- 请求之间有人类反应延迟（不是 burst）
- 不会同时开 10 个长连接
- 高峰期也不会超过每秒 3-5 个请求

**你的项目支持**：
- `concurrency.max_user_concurrent_requests` — 限制单用户并发数
- `concurrency.ping_interval` — 长连接心跳控制
- Rate limiting — 请求频率限制

**正确配置**：
```yaml
concurrency:
  # 单个 OAuth 账号同时最多 2-3 个活跃请求
  max_user_concurrent_requests: 3
  
  # 长连接超过 50 秒无数据就主动断，强制短连接
  # 避免"连接一开就 24 小时挂着"的机器人信号
  ping_interval: 50

rate_limit:
  # 单个 OAuth 账号：每分钟最多 60 个请求（足够真人用）
  requests_per_minute: 60
  # 同一 IP：每分钟最多 200 个请求（多个账号叠加）
  requests_per_minute_ip: 200
```

#### 3.2 IP / 代理 绑定

**最高风险的玩法**：
```
同一个 OAuth token，从 10 个不同的代理 IP 打请求
→ Anthropic 看到：同一设备/user_id，多地域多 ISP
→ 100% 判定机器人
```

**正确做法**：
```
OAuth 账号 A ← 绑定 → 固定代理 IP A
OAuth 账号 B ← 绑定 → 固定代理 IP B
// 每个订阅号对应一个稳定的代理 IP（residential proxy）
// 即使换代理，也要等一天再用（IP 变化被识别为"登出→重新登录"）
```

**在网关层的实现**：
```go
// 建议新增配置
type OAuthAccountConfig struct {
    Token string
    TLSFingerprintProfileID int64
    BoundIP string        // 固定出口 IP
    AllowedIPRanges []string  // 允许的客户端 IP 范围（如果是内部网关，要限制）
}

// 在 buildUpstreamRequest 时验证
func (s *GatewayService) validateIPBinding(c *gin.Context, account *OAuthAccountConfig) error {
    clientIP := c.ClientIP()
    if account.BoundIP != "" && !isInRange(clientIP, account.AllowedIPRanges) {
        return fmt.Errorf("request from %s not in allowed range for account %s", clientIP, account.Token)
    }
    return nil
}
```

#### 3.3 账号轮转与隐蔽使用

**高频用户的死法**：
```
Monday: 用账号 A
Tuesday: 用账号 B
Wednesday: 用账号 A 又复活了
→ Anthropic: "又是那个频繁轮转的家伙，封"
```

**正确做法**：
- 不要每天换账号。选定 2-3 个账号，长期固定使用其中一个
- 如果要轮转，要模仿人的"订阅取消→隔一个月→重新订阅"节奏，而不是"每天切账号"
- 定期（每周/月）让账号在真实 IP 上登 Web 版本，发一两个无关的低频请求，建立"这是活跃用户"的假象

---

## 配置清单

```yaml
# config.example.yaml 中的防封相关参数

# ========== Tier 1：TLS 指纹 ==========
# （这个通常在 Account 表的 extra 字段里配置）
accounts:
  oauth_accounts:
    - id: 1
      token: "sk-..."
      # 必须绑定一个有效的 TLS 指纹模板
      tls_fingerprint_profile_id: 1  # ID=1 是内置的 Node.js v22.22.2

# ========== Tier 2：客户端伪装 ==========
gateway:
  # 强制在所有 OAuth /v1/messages 请求前注入 billing attribution block
  # （目前代码在 applyClaudeCodeOAuthMimicryToBody 里做，要确保覆盖所有路径）
  
  # 强制伪装 Claude Code 客户端头
  # （mimicClaudeCode=true 时，已有 applyClaudeCodeMimicHeaders）
  
  # 关键：max_body_size 不要设太小，避免合法请求被截断
  max_body_size: 52428800  # 50 MB

# ========== Tier 2.5：Metadata 防关联 ==========
# 确保：
# ① metadata.user_id 总是被重写为 "{account_uuid}:{device_id}"
# ② 同一 OAuth 账号的所有请求用同一个 device_id（来自指纹）
# ③ 不要开启 metadata passthrough，除非明确知道风险

# ========== Tier 1.5：量级控制 ==========
concurrency:
  # 单账号最多 3 个并发，足够用
  max_user_concurrent_requests: 3
  # 长连接 50 秒无数据就断
  ping_interval: 50

rate_limit:
  # 单账号每分钟 60 请求（日 86400 / 1440 分钟 ≈ 60/分钟，足够）
  requests_per_minute: 60

# ========== 用户行为模拟：流量调制（可选，P1） ==========
# 模拟真实用户的活跃模式，避免 24/7 机器化操作被识别
# 默认禁用，可根据需要启用
traffic_modulation:
  enabled: false  # 设为 true 启用流量调制
  # timezone: "America/New_York"  # 可选，指定时区；默认使用全局 timezone 设置
  
# 启用后的行为：
# - 凌晨 2-6 点：降速 10%（模拟睡眠）
# - 早晨 6-9 点：降速 50%（模拟起床）
# - 工作 9-18 点：满速 100%（正常工作）
# - 下班 18-22 点：降速 30%（模拟下班）
# - 睡前 22-2 点：降速 70%（模拟睡前准备）
#
# 注意：
# ① 仅在启用时生效，禁用状态下零开销
# ② 影响范围：并发限制、超时设置、请求速率等
# ③ 需要在网关中集成应用（见 TRAFFIC_MODULATION_INTEGRATION.md）

# ========== Tier 3：逆向产物（可选，置信度低）==========
gateway_forwarding:
  # 如果要启用 CCH 签名，改为 true（但这个很可能无关）
  # enable_cch_signing: false  # 默认，可能无害也可能无用
```

---

## 故障排查

### 症状 1：请求被 Anthropic 拒绝 403 / 401

**排查顺序**：

1. **OAuth token 过期了**？
   ```bash
   curl -H "Authorization: Bearer {token}" https://api.anthropic.com/v1/messages \
     -H "Content-Type: application/json" \
     -d '{"model":"claude-3-5-sonnet-20241022","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}'
   # 如果返回 401，token 无效，需要重新登录
   ```

2. **IP 被限频了**？
   ```
   如果返回 429 Too Many Requests，说明：
   ① 同一个 token 的请求太频繁
   ② 或同一个 IP 的请求太多（多个 token 叠加）
   → 降速 or 换 IP
   ```

3. **User-Agent 不匹配**？
   ```bash
   # 检查日志中发往 Anthropic 的 header
   grep "User-Agent:" logs/gateway.log
   # 应该看到 "claude-cli/x.x.x"（如果 mimicClaudeCode=true）
   ```

4. **TLS 握手失败 / 指纹不符**？
   ```
   如果 DNS 成功但 TLS 握手失败，可能是：
   ① 代理 / VPN 干扰了握手
   ② 指纹模板格式错误（cipher_suites 顺序、extensions 列表）
   → 用 tshark / wireshark 对比官方 CLI 的握手包
   ```

### 症状 2：请求成功但很快被禁用（hours ~ days 内）

**这是 Tier 1 信号泄露**：

1. **TLS 指纹泄露** — 最可能
   - [ ] 指纹获取有没有失败？检查日志 `"Warning: failed to get fingerprint"`
   - [ ] 指纹模板的 cipher_suites 顺序对了吗？（一个字符都不能错）
   - [ ] 是不是切了代理 / VPN 导致 IP 变了，但 TLS 指纹还是 Go default？

2. **并发/速率信号泄露** — 次可能
   - [ ] 单个账号有没有超过 3 个并发？
   - [ ] 一分钟内有没有超过 60 个请求？
   - [ ] 是不是多个账号用同一个 IP？

3. **metadata 关联泄露** — 可能
   - [ ] 同一个 OAuth token，device_id 有没有变过（应该稳定）？
   - [ ] metadata.user_id 有没有被下游随意改写？

### 症状 3：账号被永久禁用（days+ 后）

**这是 Anthropic 的主动检测结果**，无法逆转。下次避免：

- 不要用黑历史 email 重新注册
- 不要批量操作（5 个账号一周内全禁）
- 不要模式过于规律（每个账号用 1 个月就切下一个）
- TLS / IP / metadata 的一致性要做好

---

## 常见误解澄清

### Q：把 `enableCCH` 改成 true 能防封吗？

**A**：不能。那是逆向猜测，大概率无关。系统 default 是 false，改成 true 反而可能因为"猜错了算法"而更坏。不要浪费精力在这个上面。

### Q：添加 "x-anthropic-billing-header" 的 system prompt block 能防封吗？

**A**：这个块的真实用途是计费归因（告诉 Anthropic "这个请求来自哪个产品"），和防封无关。有没有这个块，Anthropic 顶多是计费归因混乱，不会因此判第三方。但既然真实 CLI 有这个块，为了自洽还是要加。

### Q：多个账号用同一个代理 IP，只要 metadata.user_id 不同就行吧？

**A**：不行。Anthropic 可能同时看 IP、TLS 指纹、device_id、User-Agent 等多维度，判断"这个 IP 下有没有多个设备在用"。即使 user_id 不同，但握手指纹完全一样（都来自同一个代理）也会被识别。最安全的做法还是"一个账号绑一个代理 IP"。

### Q：同一个账号从不同 IP 打请求，但每次都伪装成同一个虚拟设备，可以吗？

**A**：不行。这等于"同一个设备从世界各地登录"，是明显的异常信号。真实场景是：用户固定在一个地方，IP 相对稳定（除了偶尔换 WiFi）。

### Q：流量调制开关在哪里？什么时候该启用？

**A**：开关位置和配置：
```yaml
# config.yaml
traffic_modulation:
  enabled: false  # 默认关闭，零开销
  timezone: "America/New_York"  # 可选，指定时区
```

**何时启用**：
- 如果账号经常被限流（429），启用流量调制可以模拟真实用户的活跃模式，降低被识别为机器的风险
- 如果只是简单的代理中转，不启用也可以（不影响正常功能）
- 启用后会根据时间段自动调整并发/超时/速率（详见配置清单中的时间段说明）

**注意**：
- 启用前需要在网关中集成应用（见 `TRAFFIC_MODULATION_INTEGRATION.md`）
- 禁用状态下完全无开销，不影响性能
- 时区配置很重要，要与用户实际所在区域对齐

---

## 项目改进 TODO

基于防封的真实需求，建议这样优化项目：

### 立即做（P0）
- [ ] 指纹获取失败时，改为返回错误而不是降级透传
- [ ] 为 OAuth 账号强制启用 metadata.user_id 重写（不受 enableMPT 影响）
- [ ] 检查所有 OAuth /v1/messages 路径是否都走过 `buildUpstreamRequest`

### 短期做（P1）
- [x] ✅ 添加流量调制功能（已实现，默认禁用）
  - 可选启用用户行为模拟（时间段调制）
  - 配置：`traffic_modulation.enabled: true/false`
  - 详见：`TRAFFIC_MODULATION_INTEGRATION.md`
  
- [x] ✅ 添加账号冷却期机制（已实现，自动启用）
  - 检测 429 时自动进入 1 小时冷却
  - 追踪 24h 冷却次数，≥3 次告警
  - 详见：代码 `account_cooldown_cache.go`
  
- [ ] 添加 OAuth 账号的 IP 绑定配置（每个账号对应一个允许的 IP 范围）
- [ ] 添加 TLS 指纹模板的版本管理（定期更新以跟上 Node.js 最新版）
- [ ] 完整集成流量调制到网关（并发/超时/速率）

### 长期做（P2）
- [ ] 支持多指纹轮转（同一账号在不同时间用不同指纹，但要避免同时使用）
- [ ] 集成 residential proxy 管理（自动分配、轮转、故障转移）
- [ ] Anthropic 的新检测机制出现后及时同步

---

## 总结

**防封的黄金法则**（按重要性）：

1. **TLS 指纹一致** — 物理层，最强信号，一旦暴露无救
2. **量级合理** — 并发 ≤ 3，速率 ≤ 60/分钟，模仿真人
3. **IP 固定** — 一个账号绑一个 IP，不要天天切
4. **metadata 稳定** — device_id / user_id 不要变，同一账号用同一虚拟设备
5. **header 自洽** — User-Agent / cc_version / system prompt 版本号对齐
6. **账号轮转合理** — 不要一周换一个，得有"订阅-取消-续费"的人类节奏

其中 1-4 是"非常关键，漏掉直接送"，5 是"最多影响自洽性判定"，6 是"长期玩法"。

**不要押宝的东西**：CCH 签名、cc_version 后缀指纹 —— 这些是逆向产物，算法可能错，影响可能被忽略。把时间投在 TLS / IP / 量级上。

