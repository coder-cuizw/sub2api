package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	accountCooldownPrefix = "account_cooldown:"
	cooldownCountPrefix   = "account_cooldown_count_24h:"
)

type CooldownInfo struct {
	CooldownUntil time.Time `json:"cooldown_until"`
	Reason        string    `json:"reason"`
	TriggerCount  int       `json:"trigger_count_24h"`
}

type AccountCooldownCache struct {
	rdb *redis.Client
}

func NewAccountCooldownCache(rdb *redis.Client) *AccountCooldownCache {
	return &AccountCooldownCache{rdb: rdb}
}

// EnterCooldown 将账号设置为冷却状态，并记录原因和触发次数
func (c *AccountCooldownCache) EnterCooldown(ctx context.Context, accountID int64, duration time.Duration, reason string) error {
	cooldownKey := fmt.Sprintf("%s%d", accountCooldownPrefix, accountID)
	cooldownUntil := time.Now().Add(duration)

	cooldownInfo := CooldownInfo{
		CooldownUntil: cooldownUntil,
		Reason:        reason,
	}

	val, err := json.Marshal(cooldownInfo)
	if err != nil {
		return err
	}

	// 在 Redis 中存储冷却状态（带 TTL）
	if err := c.rdb.Set(ctx, cooldownKey, val, duration).Err(); err != nil {
		return err
	}

	// 增加 24 小时内的冷却次数计数
	countKey := fmt.Sprintf("%s%d", cooldownCountPrefix, accountID)
	if err := c.rdb.Incr(ctx, countKey).Err(); err != nil {
		return err
	}

	// 设置计数器的 TTL 为 24 小时（首次递增时自动设置）
	c.rdb.Expire(ctx, countKey, 24*time.Hour)

	return nil
}

// IsInCooldown 检查账号是否处于冷却状态
func (c *AccountCooldownCache) IsInCooldown(ctx context.Context, accountID int64) (bool, error) {
	cooldownKey := fmt.Sprintf("%s%d", accountCooldownPrefix, accountID)
	exists, err := c.rdb.Exists(ctx, cooldownKey).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// GetCooldownInfo 获取账号的冷却信息
func (c *AccountCooldownCache) GetCooldownInfo(ctx context.Context, accountID int64) (*CooldownInfo, error) {
	cooldownKey := fmt.Sprintf("%s%d", accountCooldownPrefix, accountID)
	val, err := c.rdb.Get(ctx, cooldownKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var info CooldownInfo
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return nil, err
	}

	// 从计数器获取 24 小时内的触发次数
	countKey := fmt.Sprintf("%s%d", cooldownCountPrefix, accountID)
	count, _ := c.rdb.Get(ctx, countKey).Int()
	info.TriggerCount = count

	return &info, nil
}

// ReleaseCooldown 提前释放账号的冷却状态（仅用于管理操作）
func (c *AccountCooldownCache) ReleaseCooldown(ctx context.Context, accountID int64) error {
	cooldownKey := fmt.Sprintf("%s%d", accountCooldownPrefix, accountID)
	return c.rdb.Del(ctx, cooldownKey).Err()
}

// GetCooldown24hCount 获取账号 24 小时内的冷却触发次数
func (c *AccountCooldownCache) GetCooldown24hCount(ctx context.Context, accountID int64) (int, error) {
	countKey := fmt.Sprintf("%s%d", cooldownCountPrefix, accountID)
	count, err := c.rdb.Get(ctx, countKey).Int()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return count, nil
}
