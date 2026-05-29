package persona

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"
)

// SpeechUrge 管理AI的"发言欲"数值
type SpeechUrge struct {
	mu          sync.RWMutex
	value       float64   // 当前发言欲，范围 [0, 300]
	threshold   float64   // 触发发言的阈值
	lastDecay   time.Time // 上次衰减时间
	lastSpeakAt time.Time // 上次触发发言的时间
	lastUser    User
	repeatCount int

	// 负反馈：记录最近触发时间
	recentSpeaks []time.Time   // 滑动窗口
	speakWindow  time.Duration // 窗口大小，比如 5 分钟
	penalty      float64       // 当前惩罚值，叠加到阈值上
}

func NewSpeechUrge(threshold float64) *SpeechUrge {
	if threshold <= 0 || threshold > 300 {
		panic("invalid threshold")
	}
	return &SpeechUrge{
		threshold:   threshold,
		lastDecay:   time.Now(),
		speakWindow: 30 * time.Minute,
	}
}

func (s *SpeechUrge) calcPenalty() float64 {
	now := time.Now()
	cutoff := now.Add(-s.speakWindow)

	valid := make([]time.Time, 0, len(s.recentSpeaks))
	for _, t := range s.recentSpeaks {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	s.recentSpeaks = valid

	if len(s.recentSpeaks) < 2 {
		return 0
	}

	// 计算窗口内相邻发言的平均间隔
	var totalGap float64
	for i := 1; i < len(s.recentSpeaks); i++ {
		totalGap += s.recentSpeaks[i].Sub(s.recentSpeaks[i-1]).Seconds()
	}
	avgGap := totalGap / float64(len(s.recentSpeaks)-1)

	// 间隔越短惩罚越重，窗口时间的一半以上无惩罚，趋近0秒时接近上限
	// log曲线保证前段涨快后段趋缓
	if avgGap >= s.speakWindow.Seconds()/2 {
		return 0
	}
	penalty := math.Log(30*60/avgGap+1) * 30
	return math.Min(penalty, 80)
}

// calcDelta 根据新消息计算发言欲增量
func (s *SpeechUrge) calcDelta(msg GroupMessage, recentCount int, isToMe bool) float64 {
	var delta float64

	// 1. 消息类型加成
	switch msg.MsgType {
	case MsgTypeText:
		delta += 0.5
	case MsgTypeAt:
		delta += 1.0
	case MsgTypeReply:
		delta += 2.0
	default:
		delta += 0.8
	}

	if l := runeLen(msg.Content); l > 0 {
		delta += math.Min(math.Log(float64(l+1))*2.0, 3.0)
	}

	switch {
	case isToMe:
		delta += 3.0
	case s.lastUser != msg.User:
		s.lastUser = msg.User
		s.repeatCount = 0
		delta += 1.0
	case s.lastUser == msg.User:
		s.repeatCount++
		delta += math.Min(float64(s.repeatCount), 6.0)

	}

	// 2. 活跃度加成：最近 N 条消息越多，增量越大
	//    recentCount = 过去 120 秒内的消息数
	activityBonus := math.Min(math.Sqrt(float64(recentCount))*2.0, 10.0)
	delta += activityBonus

	// 3. 寂寞加成：距离上次发言越久，增量越大
	silentDuration := time.Since(s.lastSpeakAt).Minutes()
	// 沉默60分钟开始生效，最多加8点
	lonelyBonus := math.Min(math.Max(silentDuration-60, 0)*0.2, 8.0)
	delta += lonelyBonus

	delta += rand.Float64()

	return delta
}

// applyDecay 对发言欲做时间衰减（每秒自然冷却）
func (s *SpeechUrge) applyDecay() {
	now := time.Now()
	elapsed := now.Sub(s.lastDecay).Seconds()
	// 每秒衰减 0.3 点，群沉默时 AI 也会逐渐"冷静"
	s.value = math.Max(0, s.value-elapsed*0.3)
	s.lastDecay = now
}

// Update 在每次 UpdateContext 后调用，返回 true 表示应触发发言
func (s *SpeechUrge) Update(msg GroupMessage, recentCount int, isToMe bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyDecay()
	delta := s.calcDelta(msg, recentCount, isToMe)
	s.value = math.Min(300, s.value+delta)

	penalty := s.calcPenalty()
	effectiveThreshold := s.threshold + penalty

	if isToMe {
		s.value *= 0.75
		s.lastSpeakAt = time.Now()
		s.recentSpeaks = append(s.recentSpeaks, time.Now())
		return true
	}

	if s.value >= effectiveThreshold {
		s.value = s.threshold * 0.1 // 保留10%，而不是归零
		s.lastSpeakAt = time.Now()
		s.recentSpeaks = append(s.recentSpeaks, time.Now())
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
