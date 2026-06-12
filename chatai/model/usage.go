package model

import (
	"gorm.io/gorm"
	"time"
)

type TokenUsage struct {
	Id          int64 `gorm:"column:id;primaryKey;autoIncrement"`
	InputToken  int64
	OutputToken int64
	ModelName   string
	TTL         time.Duration
	CreateAt    time.Time
}

func (t *TokenUsage) Insert(db *gorm.DB) error {
	return db.Create(t).Error
}
