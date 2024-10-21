package chatai

import (
	"gorm.io/gorm"
	"time"
)

type UsageRecord struct {
	UserId         int64 `gorm:"primaryKey"`
	GroupId        int64 `gorm:"primaryKey"`
	UseInputToken  int64
	UseOutputToken int64
	// 只有在刷新的时候更新
	LastUpdate time.Time
}

// Allow 判断是否允许使用
func (r *UsageRecord) Allow(db *gorm.DB, limitInput int64, limitOutput int64) (bool, error) {
	err := db.First(&r).Error
	if err != nil {
		return false, err
	}

	now := time.Now()
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if todayMidnight.Sub(r.LastUpdate) >= 24*time.Hour {
		// 如果 LastUpdate 过了24h，重置 token
		r.UseInputToken = 0
		r.UseOutputToken = 0
		r.LastUpdate = todayMidnight
		err = db.Save(&r).Error
		if err != nil {
			return false, err
		}
	}
	return r.UseInputToken < limitInput || r.UseOutputToken < limitOutput, nil
}
