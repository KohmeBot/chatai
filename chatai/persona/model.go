package persona

import (
	"errors"
	"gorm.io/gorm"
)

type ChatJson struct {
	Text              string       `json:"text"`
	AtTarget          int64        `json:"atTarget"`
	ReplayMsg         int64        `json:"replayMsg"`
	NewAbstract       string       `json:"newAbstract"`
	Favor             int64        `json:"favor"`
	PokeTarget        int64        `json:"pokeTarget"`
	UpdateImpressions []Impression `json:"updateImpressions,omitempty"`
}

type Impression struct {
	UserID  int64  `json:"userId"`
	Content string `json:"content"` // 对这个人的印象，400字以内
}

func (i Impression) String() string {
	if i.Content == "" {
		return "你对他没有任何印象"
	}
	return i.Content
}

type UserImpression struct {
	GroupID int64  `gorm:"index:idx_group_user,unique"`
	UserID  int64  `gorm:"index:idx_group_user,unique"`
	Content string // 印象内容
}

func (u *UserImpression) Update(db *gorm.DB, group int64, i Impression) error {
	return db.Where(UserImpression{GroupID: group, UserID: i.UserID}).
		Assign(UserImpression{Content: i.Content}).
		FirstOrCreate(u).Error
}

func (u *UserImpression) Get(db *gorm.DB, group int64, user int64) (Impression, error) {
	err := db.Where(UserImpression{GroupID: group, UserID: user}).First(u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Impression{UserID: user}, nil
	}
	if err != nil {
		return Impression{}, err
	}
	return Impression{
		UserID:  u.UserID,
		Content: u.Content,
	}, nil
}
