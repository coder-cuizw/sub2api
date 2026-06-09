package service

import (
	"time"
)

// TrafficModulationService 管理基于活跃时间段的请求速率调制
// 用于模拟真实用户的行为模式，防止 24/7 机器化操作被识别
type TrafficModulationService struct {
	timeWindows []TimeWindow
	timezone    *time.Location
}

// TimeWindow 定义一个时间窗口内的速率调制因子
type TimeWindow struct {
	StartHour         int     // 起始小时（0-23，UTC）
	EndHour           int     // 结束小时（0-23，UTC）
	ModulationFactor  float64 // 调制因子（0.0-1.0），1.0 表示满速
	Description       string  // 时间段描述（如 "sleep", "work", "break"）
}

// DefaultTrafficModulationWindows 返回默认的时间窗口配置
// 假设用户在美国东部时区，模拟真实用户的活跃模式
func DefaultTrafficModulationWindows() []TimeWindow {
	return []TimeWindow{
		{
			StartHour:        2,
			EndHour:          6,
			ModulationFactor: 0.1,
			Description:      "sleep",
		},
		{
			StartHour:        6,
			EndHour:          9,
			ModulationFactor: 0.5,
			Description:      "wake_up",
		},
		{
			StartHour:        9,
			EndHour:          18,
			ModulationFactor: 1.0,
			Description:      "work",
		},
		{
			StartHour:        18,
			EndHour:          22,
			ModulationFactor: 0.7,
			Description:      "off_work",
		},
		{
			StartHour:        22,
			EndHour:          2,
			ModulationFactor: 0.3,
			Description:      "before_sleep",
		},
	}
}

// NewTrafficModulationService 创建新的流量调制服务
func NewTrafficModulationService(timeWindows []TimeWindow, tzName string) (*TrafficModulationService, error) {
	if len(timeWindows) == 0 {
		timeWindows = DefaultTrafficModulationWindows()
	}

	tz := time.UTC
	if tzName != "" {
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			// 忽略时区加载错误，回退到 UTC
			tz = time.UTC
		} else {
			tz = loc
		}
	}

	return &TrafficModulationService{
		timeWindows: timeWindows,
		timezone:    tz,
	}, nil
}

// GetModulationFactor 获取当前时刻的速率调制因子
// 返回值在 0.0-1.0 之间，其中 1.0 表示满速（无限制）
func (s *TrafficModulationService) GetModulationFactor() float64 {
	if s == nil || len(s.timeWindows) == 0 {
		return 1.0 // 默认满速
	}

	now := time.Now().In(s.timezone)
	hour := now.Hour()

	for _, window := range s.timeWindows {
		if window.isInWindow(hour) {
			return window.ModulationFactor
		}
	}

	// 如果没有匹配到任何窗口，返回默认的满速
	return 1.0
}

// GetModulationFactorForTime 获取指定时刻的速率调制因子
// 用于测试或特殊场景
func (s *TrafficModulationService) GetModulationFactorForTime(t time.Time) float64 {
	if s == nil || len(s.timeWindows) == 0 {
		return 1.0
	}

	hour := t.In(s.timezone).Hour()

	for _, window := range s.timeWindows {
		if window.isInWindow(hour) {
			return window.ModulationFactor
		}
	}

	return 1.0
}

// GetTimeWindowDescription 获取当前时间窗口的描述
func (s *TrafficModulationService) GetTimeWindowDescription() string {
	if s == nil || len(s.timeWindows) == 0 {
		return "default"
	}

	now := time.Now().In(s.timezone)
	hour := now.Hour()

	for _, window := range s.timeWindows {
		if window.isInWindow(hour) {
			return window.Description
		}
	}

	return "unknown"
}

// isInWindow 检查小时是否在时间窗口内
// 处理跨午夜的情况（如 22-2）
func (w TimeWindow) isInWindow(hour int) bool {
	if w.StartHour < w.EndHour {
		// 正常情况：如 9-18
		return hour >= w.StartHour && hour < w.EndHour
	}
	// 跨午夜情况：如 22-2
	return hour >= w.StartHour || hour < w.EndHour
}

// ApplyModulationToConcurrency 对并发限制应用调制因子
// 例如：如果基础并发为 10，调制因子为 0.5，则返回 5
func ApplyModulationToConcurrency(baseConcurrency int, modulationFactor float64) int {
	if baseConcurrency <= 0 || modulationFactor < 0 {
		return 0
	}
	if modulationFactor >= 1.0 {
		return baseConcurrency
	}
	modulated := int(float64(baseConcurrency) * modulationFactor)
	if modulated < 1 && baseConcurrency > 0 {
		return 1 // 至少允许 1 个并发
	}
	return modulated
}

// ApplyModulationToTimeout 对超时时间应用调制因子
// 调制因子越小，超时越短（请求处理时间更短，模拟高频操作）
// 例如：如果基础超时为 30s，调制因子为 0.5，则返回 15s
func ApplyModulationToTimeout(baseTimeout time.Duration, modulationFactor float64) time.Duration {
	if baseTimeout <= 0 || modulationFactor <= 0 {
		return baseTimeout
	}
	if modulationFactor >= 1.0 {
		return baseTimeout
	}
	return time.Duration(float64(baseTimeout) * modulationFactor)
}

// ApplyModulationToRPM 对每分钟请求数应用调制因子
// 例如：如果基础 RPM 为 100，调制因子为 0.5，则返回 50
func ApplyModulationToRPM(baseRPM int, modulationFactor float64) int {
	if baseRPM <= 0 || modulationFactor < 0 {
		return 0
	}
	if modulationFactor >= 1.0 {
		return baseRPM
	}
	modulated := int(float64(baseRPM) * modulationFactor)
	if modulated < 1 && baseRPM > 0 {
		return 1 // 至少允许 1 个请求
	}
	return modulated
}
