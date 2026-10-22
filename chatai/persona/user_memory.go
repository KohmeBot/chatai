package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kohmebot/chatai/chatai/favor"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserMemory is the canonical store for both knowledge and relationship state.
// Legacy impression/favor tables are retained only for migration.
type UserMemory struct {
	UserID      int64 `gorm:"primaryKey"`
	ProfileJSON string
	Score       int64
	Version     uint64
	UpdatedAt   time.Time
}

type MemoryItem struct {
	Key        string     `json:"key"`
	Kind       string     `json:"kind"` // fact, preference, boundary, state, experience
	Text       string     `json:"text"`
	SourceID   uint       `json:"source_id"`
	GroupID    int64      `json:"group_id"`
	ObservedAt time.Time  `json:"observed_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Confidence string     `json:"confidence"` // explicit or inferred
}

type RelationshipMemory struct {
	Score       int64  `json:"score"`
	Level       string `json:"level"`
	Description string `json:"description,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type UserProfile struct {
	UserID        int64              `json:"user_id"`
	Version       uint64             `json:"version"`
	ManualThrough uint               `json:"manual_through,omitempty"`
	Summary       string             `json:"summary,omitempty"`
	Items         []MemoryItem       `json:"items"`
	Relationship  RelationshipMemory `json:"relationship"`
}

func readUserMemory(db *gorm.DB, userID int64) (UserProfile, error) {
	if userID <= 0 {
		return UserProfile{}, errors.New("user_id must be positive")
	}
	var row UserMemory
	err := db.First(&row, "user_id = ?", userID).Error
	profile := UserProfile{UserID: userID, Items: []MemoryItem{}}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		profile.Relationship.Level = favor.GetFavorLevelInfo(0).Name
		return profile, nil
	}
	if err != nil {
		return profile, err
	}
	if err := json.Unmarshal([]byte(row.ProfileJSON), &profile); err != nil {
		return profile, fmt.Errorf("decode user memory: %w", err)
	}
	profile.UserID, profile.Version = row.UserID, row.Version
	profile.Relationship.Score = row.Score
	profile.Relationship.Level = favor.GetFavorLevelInfo(row.Score).Name
	return profile, nil
}

// CAS is enforced by the database, including concurrent first writes.
func writeUserMemory(db *gorm.DB, profile UserProfile) error {
	version := profile.Version
	profile.Version++
	profile.Relationship.Score = max(favor.FavorMin, min(favor.FavorMax, profile.Relationship.Score))
	profile.Relationship.Level = favor.GetFavorLevelInfo(profile.Relationship.Score).Name
	encoded, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	row := UserMemory{UserID: profile.UserID, ProfileJSON: string(encoded), Score: profile.Relationship.Score, Version: profile.Version, UpdatedAt: time.Now()}
	if version == 0 {
		// A plain INSERT is intentional: MySQL CLIENT_FOUND_ROWS can report a
		// no-op upsert as one affected row, which is not a successful CAS.
		if err := db.Create(&row).Error; err != nil {
			return fmt.Errorf("user memory changed or could not be created; read it again before updating: %w", err)
		}
		return nil
	}
	result := db.Model(&UserMemory{}).Where("user_id = ? AND version = ?", row.UserID, version).
		Updates(map[string]any{"profile_json": row.ProfileJSON, "score": row.Score, "version": row.Version, "updated_at": row.UpdatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("user memory changed; read it again before updating")
	}
	return nil
}

// MigrateUserMemories never overwrites an existing canonical profile.
func MigrateUserMemories(db *gorm.DB) error {
	insert := func(userID, score int64, summary string) error {
		profile := UserProfile{UserID: userID, Version: 1, Summary: summary, Items: []MemoryItem{},
			Relationship: RelationshipMemory{Score: score, Level: favor.GetFavorLevelInfo(score).Name}}
		data, err := json.Marshal(profile)
		if err != nil {
			return err
		}
		return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&UserMemory{
			UserID: userID, Score: score, Version: 1, ProfileJSON: string(data), UpdatedAt: time.Now(),
		}).Error
	}
	var impressions []UserImpression
	if err := db.FindInBatches(&impressions, 100, func(tx *gorm.DB, _ int) error {
		for _, old := range impressions {
			var relation favor.FavorRecord
			err := db.First(&relation, "user_id = ?", old.UserID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := insert(old.UserID, relation.Favor, old.Content); err != nil {
				return err
			}
		}
		return nil
	}).Error; err != nil {
		return err
	}
	var relations []favor.FavorRecord
	return db.FindInBatches(&relations, 100, func(tx *gorm.DB, _ int) error {
		for _, old := range relations {
			if err := insert(old.UserId, old.Favor, ""); err != nil {
				return err
			}
		}
		return nil
	}).Error
}

type MemoryChange struct {
	UserID          int64        `json:"user_id"`
	PreviousVersion uint64       `json:"previous_version"`
	EvidenceIDs     []uint       `json:"evidence_ids"`
	Reason          string       `json:"reason"`
	Summary         *string      `json:"summary,omitempty"`
	Upsert          []MemoryItem `json:"upsert,omitempty"`
	Forget          []string     `json:"forget,omitempty"`
	Relationship    *struct {
		Delta       int64  `json:"delta"`
		Description string `json:"description"`
	} `json:"relationship,omitempty"`
}

func applyMemoryChange(profile UserProfile, change MemoryChange, messages []ChatMessageRecord, groupID, selfID int64, now time.Time) (UserProfile, error) {
	if change.UserID <= 0 || change.UserID == selfID || change.UserID != profile.UserID || change.PreviousVersion != profile.Version {
		return profile, errors.New("invalid user or stale memory version")
	}
	if strings.TrimSpace(change.Reason) == "" || utf8.RuneCountInString(change.Reason) > 200 {
		return profile, errors.New("reason must contain 1 to 200 characters")
	}
	evidence := make(map[uint]ChatMessageRecord)
	for _, msg := range messages {
		// Another member's claims and quoted text cannot establish this user's facts.
		if msg.GroupID == groupID && msg.UserID == change.UserID {
			evidence[msg.ID] = msg
		}
	}
	if len(change.EvidenceIDs) == 0 || len(change.EvidenceIDs) > 20 {
		return profile, errors.New("1 to 20 source evidence IDs required")
	}
	selected := make(map[uint]ChatMessageRecord)
	for _, id := range change.EvidenceIDs {
		msg, ok := evidence[id]
		if !ok {
			return profile, errors.New("evidence must be this user's messages in this group")
		}
		selected[id] = msg
	}
	return applyMemoryContent(profile, change, selected, groupID, now)
}

// Shared bounded merging for user and group memory contents.
func applyMemoryContent(profile UserProfile, change MemoryChange, selected map[uint]ChatMessageRecord, groupID int64, now time.Time) (UserProfile, error) {
	if change.Summary != nil {
		if utf8.RuneCountInString(*change.Summary) > 600 {
			return profile, errors.New("summary exceeds 600 characters")
		}
		profile.Summary = strings.TrimSpace(*change.Summary)
	}
	if len(change.Upsert) > 20 || len(change.Forget) > 30 {
		return profile, errors.New("too many memory changes")
	}
	items := make([]MemoryItem, 0, len(profile.Items)+len(change.Upsert))
	for _, item := range profile.Items {
		if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
			continue
		}
		forgotten := false
		for _, key := range change.Forget {
			forgotten = forgotten || item.Key == key
		}
		if !forgotten {
			items = append(items, item)
		}
	}
	for _, item := range change.Upsert {
		source, ok := selected[item.SourceID]
		if !ok {
			return profile, errors.New("item source_id must be included in evidence_ids")
		}
		switch item.Kind {
		case "fact", "preference", "boundary", "state", "experience":
		default:
			return profile, errors.New("invalid memory kind")
		}
		if strings.TrimSpace(item.Key) == "" || utf8.RuneCountInString(item.Key) > 64 || strings.TrimSpace(item.Text) == "" || utf8.RuneCountInString(item.Text) > 200 {
			return profile, errors.New("invalid memory key or text length")
		}
		if item.Confidence != "explicit" && item.Confidence != "inferred" {
			return profile, errors.New("confidence must be explicit or inferred")
		}
		item.GroupID, item.ObservedAt = groupID, source.CreatedAt
		if item.Kind == "state" && item.ExpiresAt == nil {
			expiry := source.CreatedAt.Add(14 * 24 * time.Hour)
			item.ExpiresAt = &expiry
		}
		if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
			continue
		}
		replaced := false
		for i := range items {
			if items[i].Key == item.Key {
				items[i], replaced = item, true
				break
			}
		}
		if !replaced {
			items = append(items, item)
		}
	}
	if len(items) > 30 {
		return profile, errors.New("at most 30 memory items; forget obsolete items first")
	}
	profile.Items = items
	if change.Relationship != nil {
		if change.Relationship.Delta < -5 || change.Relationship.Delta > 5 || utf8.RuneCountInString(change.Relationship.Description) > 200 {
			return profile, errors.New("relationship delta must be -5..5 and description at most 200 characters")
		}
		profile.Relationship.Score = max(favor.FavorMin, min(favor.FavorMax, profile.Relationship.Score+change.Relationship.Delta))
		profile.Relationship.Level = favor.GetFavorLevelInfo(profile.Relationship.Score).Name
		profile.Relationship.Description = change.Relationship.Description
		profile.Relationship.Reason = change.Reason
	}
	return profile, nil
}

// Keep automatic input bounded; full, unexpired memory remains available by tool.
func visibleUserProfile(profile UserProfile, groupID int64, now time.Time, limit int) UserProfile {
	items := make([]MemoryItem, 0)
	for _, item := range profile.Items {
		if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
			continue
		}
		// Shared preferences are global; events/states stay in their source group.
		if (item.Kind == "state" || item.Kind == "experience") && item.GroupID != groupID {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		priority := func(item MemoryItem) int {
			switch item.Kind {
			case "boundary":
				return 3
			case "preference":
				return 2
			default:
				return 1
			}
		}
		if priority(items[i]) != priority(items[j]) {
			return priority(items[i]) > priority(items[j])
		}
		return items[i].ObservedAt.After(items[j].ObservedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	profile.Items = items
	if runes := []rune(profile.Summary); len(runes) > 600 {
		profile.Summary = string(runes[:600])
	}
	return profile
}
