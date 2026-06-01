package persona

import (
	"errors"
	"gorm.io/gorm"
)

type ChatJson struct {
	Messages          []Message        `json:"messages"`
	NewAbstract       string           `json:"newAbstract"`
	Favor             int64            `json:"favor"`
	GroupImpression   string           `json:"groupImpression"`
	UpdateImpressions []UserImpression `json:"updateImpressions,omitempty"`
}

type Message struct {
	Text       string `json:"text"`
	AtTarget   int64  `json:"atTarget"`
	ReplayMsg  int64  `json:"replayMsg"`
	PokeTarget int64  `json:"pokeTarget"`
}

type UserImpression struct {
	UserID  int64  ` json:"userId" gorm:"primaryKey" `
	Content string `json:"content"` // 印象内容
}

func (u *UserImpression) String() string {
	if u.Content == "" {
		return "你对他没有任何印象"
	}
	return u.Content
}

func (u *UserImpression) Update(db *gorm.DB, i UserImpression) error {
	if i.Content == "" {
		return nil
	}
	return db.Where(UserImpression{UserID: i.UserID}).
		Assign(UserImpression{Content: i.Content}).
		FirstOrCreate(u).Error
}

func (u *UserImpression) Get(db *gorm.DB, user int64) (UserImpression, error) {
	err := db.Where(UserImpression{UserID: user}).First(u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return UserImpression{UserID: user}, nil
	}
	if err != nil {
		return UserImpression{}, err
	}
	return UserImpression{
		UserID:  u.UserID,
		Content: u.Content,
	}, nil
}

type GroupImpression struct {
	GroupID int64  `gorm:"primaryKey"`
	Content string // 印象内容
}

func (g *GroupImpression) String() string {
	if g.Content == "" {
		return "你对该群聊没有任何印象"
	}
	return g.Content
}

func (g *GroupImpression) Update(db *gorm.DB, i GroupImpression) error {
	if i.Content == "" {
		return nil
	}

	return db.Where(GroupImpression{GroupID: i.GroupID}).
		Assign(GroupImpression{Content: i.Content}).
		FirstOrCreate(g).Error
}

func (g *GroupImpression) Get(db *gorm.DB, group int64) (GroupImpression, error) {
	err := db.Where(GroupImpression{GroupID: group}).First(g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return GroupImpression{GroupID: group}, nil
	}
	if err != nil {
		return GroupImpression{}, err
	}
	return GroupImpression{
		GroupID: g.GroupID,
		Content: g.Content,
	}, nil
}
