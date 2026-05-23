package favor

import (
	"encoding/json"
	"fmt"
	"github.com/kohmebot/chatai/chatai/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/rand/v2"
)

const (
	FavorDefault = 65
	FavorMax     = 1000
	FavorMin     = -100
)

const (
	AddMax = 60
	SubMax = 100
)

const favorFormat = "你的回复要以json的格式给我,字段有两个,第一个是answer(string)这是你回答的内容,第二个是favor(int)这是你根据提问给用户增加或者减少的好感度,每次最多增加%d点,减少%d点,接下来我会让你扮演角色和群友对话,我会告诉你当前你对他的好感度(%d到%d),请根据好感度来回答问题\n%s"

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

func GetFavor(db *gorm.DB, uid int64) (value int64, info LevelInfo, err error) {
	f := FavorRecord{UserId: uid}

	value, err = f.Get(db)
	if err != nil {
		return
	}
	info = GetFavorLevelInfo(value)
	return
}

func ProcessFavorResponse(db *gorm.DB, uid int64, response *model.Response) error {
	var f FavorResponse
	err := json.Unmarshal([]byte(response.Answer), &f)
	if err != nil {
		return err
	}
	response.Answer = f.Answer
	r := FavorRecord{UserId: uid}
	return r.Add(db, f.Favor)
}
