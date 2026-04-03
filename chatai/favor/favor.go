package favor

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FavorMax = 1000
	FavorMin = -100
)

const (
	AddMax = 60
	SubMax = 100
)

const favorFormat = "你的回复要以json的格式给我,字段有两个,第一个是answer(string)这是你回答的内容,第二个是favor(int)这是你根据提问给用户增加或者减少的好感度,每次最多增加%d点,减少%d点,接下来我会让你扮演角色和群友对话,我会告诉你当前你对他的好感度(%d-%d),根据好感度的多少来回答问题\n%s"

type Favor struct {
	Enable bool
}

func (f Favor) WithSystem(system string) string {
	if !f.Enable {
		return system
	}
	s := fmt.Sprintf(favorFormat, AddMax, SubMax, FavorMin, FavorMax, system)
	return s
}

type FavorResponse struct {
	Answer string `json:"answer"`
	Favor  int64  `json:"favor"`
}

type FavorRecord struct {
	UserId int64 `gorm:"primaryKey"`
	Favor  int64
}

func clampFavor(v int64) int64 {
	if v > FavorMax {
		return FavorMax
	}
	if v < FavorMin {
		return FavorMin
	}
	return v
}

func (f *FavorRecord) Add(db *gorm.DB, delta int64) error {
	if delta > AddMax {
		delta = AddMax
	}
	if delta < -SubMax {
		delta = -SubMax
	}

	return db.Transaction(func(tx *gorm.DB) error {
		var record FavorRecord

		err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", f.UserId).
			First(&record).
			Error

		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 不存在就初始化
				record = FavorRecord{
					UserId: f.UserId,
					Favor:  clampFavor(delta),
				}
				return tx.Create(&record).Error
			}
			return err
		}

		// 计算 + clamp
		newFavor := clampFavor(record.Favor + delta)

		return tx.Model(&record).
			Update("favor", newFavor).
			Error
	})
}

func (f *FavorRecord) Get(db *gorm.DB) (int64, error) {
	var record FavorRecord

	err := db.
		Where("user_id = ?", f.UserId).
		First(&record).
		Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}

	return record.Favor, nil
}
