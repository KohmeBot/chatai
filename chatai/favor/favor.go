package favor

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/rand/v2"
)

const (
	FavorDefault = 400
	FavorMax     = 1000
	FavorMin     = -100
)

const (
	AddMax = 60
	SubMax = 100
)

type FavorRecord struct {
	UserId int64 `gorm:"primaryKey"`
	Favor  int64
}

func getDefaultFavor() int64 {
	return FavorDefault + rand.Int64N(51) - 25
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
			Where(FavorRecord{UserId: f.UserId}).
			Attrs(FavorRecord{Favor: getDefaultFavor()}).
			FirstOrCreate(&record).Error
		if err != nil {
			return err
		}
		newFavor := clampFavor(record.Favor + delta)
		return tx.Model(&record).
			Where("user_id = ?", f.UserId). // 显式指定，不依赖 record 的主键
			Update("favor", newFavor).Error
	})
}

func (f *FavorRecord) Get(db *gorm.DB) (int64, error) {
	var record FavorRecord

	err := db.
		Where(FavorRecord{UserId: f.UserId}).
		Attrs(FavorRecord{Favor: getDefaultFavor()}).
		FirstOrCreate(&record).
		Error

	if err != nil {
		return 0, err
	}

	return record.Favor, nil
}

func GetFavor(db *gorm.DB, uid int64) (value int64, err error) {
	f := FavorRecord{UserId: uid}

	value, err = f.Get(db)
	if err != nil {
		return
	}

	return
}

func UpdateFavor(db *gorm.DB, uid int64, delta int64) error {
	r := FavorRecord{UserId: uid}
	return r.Add(db, delta)
}
