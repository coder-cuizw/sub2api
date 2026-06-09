# 流量调制集成指南

## 概述

`TrafficModulationService` 提供基于时间的请求速率调制，用于模拟真实用户行为模式。

## 服务特性

### 默认时间窗口（UTC 时区）

```
凌晨 2-6 点   →  10%  速率（睡眠）
早晨 6-9 点   →  50%  速率（起床）
工作 9-18 点  → 100%  速率（活跃）
下班 18-22 点 →  70%  速率（下班）
睡前 22-2 点  →  30%  速率（准备睡眠）
```

### 配置

时间窗口配置在 `internal/service/traffic_modulation_service.go` 中，可以通过修改 `DefaultTrafficModulationWindows()` 函数调整。

时区配置来自 `config.yaml` 中的 `timezone` 字段：

```yaml
timezone: "America/New_York"  # 支持任何有效的 IANA 时区
```

## 使用方式

### 1. 获取调制因子

```go
modulation := trafficModulationService.GetModulationFactor()
// modulation 取值范围: 0.0 - 1.0
```

### 2. 应用到并发限制

```go
baseConcurrency := 10
modulatedConcurrency := ApplyModulationToConcurrency(baseConcurrency, modulation)
// 例如：modulation=0.5 时，结果为 5
```

### 3. 应用到超时

```go
baseTimeout := 30 * time.Second
modulatedTimeout := ApplyModulationToTimeout(baseTimeout, modulation)
// 例如：modulation=0.5 时，结果为 15s
```

### 4. 应用到 RPM 限流

```go
baseRPM := 100
modulatedRPM := ApplyModulationToRPM(baseRPM, modulation)
// 例如：modulation=0.5 时，结果为 50
```

## 集成路径（推荐）

### 方案 A：并发限制集成（高优先级）

在 `GatewayService` 中选择账号时应用调制：

```go
// 在网关获取账号时
account, err := s.selectAccount(...)

// 获取调制因子
modulation := s.trafficModulationService.GetModulationFactor()

// 应用到并发限制
accountConcurrency := account.Concurrency
actualConcurrency := ApplyModulationToConcurrency(accountConcurrency, modulation)

// 传递给并发控制服务
s.concurrencyService.Reserve(account.ID, actualConcurrency)
```

### 方案 B：超时集成（中优先级）

在 `buildUpstreamRequest` 方法中应用调制：

```go
modulation := s.trafficModulationService.GetModulationFactor()
responseHeaderTimeout := time.Duration(s.cfg.Gateway.ResponseHeaderTimeout) * time.Second
actualTimeout := ApplyModulationToTimeout(responseHeaderTimeout, modulation)

// 应用到 HTTP 客户端配置
httpClient.Timeout = actualTimeout
```

### 方案 C：RPM 限流集成（低优先级）

在 `RateLimitService` 中应用调制：

```go
modulation := s.trafficModulationService.GetModulationFactor()
baseRPM := account.RPMLimit
actualRPM := ApplyModulationToRPM(baseRPM, modulation)

// 在速率限制检查中使用
if currentRequestCount > actualRPM {
    // 触发限流
}
```

## 监控和日志

### 推荐的监控指标

1. **调制因子变化**：记录时间窗口变化
2. **实际并发 vs 基础并发**：追踪调制效果
3. **请求分布**：验证是否符合预期的活跃模式

### 日志示例

```go
modulation := s.trafficModulationService.GetModulationFactor()
windowDesc := s.trafficModulationService.GetTimeWindowDescription()

slog.Info(
    "traffic_modulation_applied",
    "window", windowDesc,
    "modulation_factor", modulation,
    "base_concurrency", account.Concurrency,
    "actual_concurrency", actualConcurrency,
)
```

## 效果评估

### 预期行为

- **低峰期（凌晨）**：请求数量减少 90%，并发降低
- **工作时间**：正常速率和并发
- **过渡时段**：逐步变化的请求模式

### 检测成功的指标

1. 请求时间分布符合配置的时间窗口
2. Anthropic API 不再报告异常的 24/7 活跃模式
3. 账号延续时间延长

## 注意事项

1. **时区配置很关键**：确保时区设置与目标用户区域对齐
2. **调制因子不会低于 1**：至少保留 1 个并发或 1 个 RPM
3. **渐进式集成**：先从并发限制开始，再逐步添加其他维度
4. **监控调制效果**：验证调制是否真正生效

## 实现检查清单

- [ ] 在 GatewayService 中注入 TrafficModulationService
- [ ] 在获取账号时应用并发调制
- [ ] 添加日志记录调制因子变化
- [ ] 在超时设置中应用调制（可选）
- [ ] 在 RPM 限流中应用调制（可选）
- [ ] 添加单元测试验证调制计算
- [ ] 配置正确的时区
- [ ] 监控实际请求分布

## 示例代码

完整的集成示例可参考：
- `internal/service/traffic_modulation_service.go` - 服务实现
- `internal/service/traffic_modulation_service_test.go` - 单元测试（待添加）
