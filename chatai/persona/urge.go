package persona

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// SpeechUrge 管理AI的"发言欲"数值
type SpeechUrge struct {
	mu        sync.RWMutex
	value     float64   // 当前发言欲，范围 [0, 100]
	threshold float64   // 触发发言的阈值
	lastDecay time.Time // 上次衰减时间
}

func NewSpeechUrge(threshold float64) *SpeechUrge {
	if threshold <= 0 || threshold > 100 {
		panic("invalid threshold")
	}
	return &SpeechUrge{
		threshold: threshold,
		lastDecay: time.Now(),
	}
}

// calcDelta 根据新消息计算发言欲增量
func (s *SpeechUrge) calcDelta(msg GroupMessage, recentCount int, isToMe bool) float64 {
	var delta float64

	// 1. 消息类型加成
	switch msg.MsgType {
	case MsgTypeText:
		delta += 3.0
	case MsgTypeImg, MsgTypeRecord:
		delta += 4.0
	case MsgTypeReply:
		delta += 5.0
	default:
		delta += 2.0
	}

	if isToMe {
		delta += 5.0
	}

	// 2. 活跃度加成：最近 N 条消息越多，增量越大
	//    recentCount = 过去 120 秒内的消息数
	activityBonus := math.Min(float64(recentCount)*0.4, 10.0)
	delta += activityBonus

	delta += rand.Float64() * 5

	return delta
}

// applyDecay 对发言欲做时间衰减（每秒自然冷却）
func (s *SpeechUrge) applyDecay() {
	now := time.Now()
	elapsed := now.Sub(s.lastDecay).Seconds()
	// 每秒衰减 0.5 点，群沉默时 AI 也会逐渐"冷静"
	s.value = math.Max(0, s.value-elapsed*0.5)
	s.lastDecay = now
}

// Update 在每次 UpdateContext 后调用，返回 true 表示应触发发言
func (s *SpeechUrge) Update(msg GroupMessage, recentCount int, isToMe bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyDecay()
	delta := s.calcDelta(msg, recentCount, isToMe)
	s.value = math.Min(100, s.value+delta)

	if s.value >= s.threshold {
		s.value = 0
		return true
	}

	if isToMe {
		return true
	}

	return false
}

// Value 返回当前发言欲（用于日志/调试）
func (s *SpeechUrge) Value() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}
