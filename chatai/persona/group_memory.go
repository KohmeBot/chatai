package persona

import (
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
	"unicode/utf8"
)

// Empty versioned rows are tombstones; legacy migration must not resurrect them.
type GroupMemory struct {
	GroupID     int64 `gorm:"primaryKey"`
	ProfileJSON string
	Version     uint64
	UpdatedAt   time.Time
}
type GroupProfile struct {
	GroupID       int64        `json:"group_id"`
	Version       uint64       `json:"version"`
	ManualThrough uint         `json:"manual_through,omitempty"`
	Summary       string       `json:"summary,omitempty"`
	Items         []MemoryItem `json:"items"`
	UpdatedAt     time.Time    `json:"updated_at"`
}
type GroupMemoryChange struct {
	PreviousVersion uint64       `json:"previous_version"`
	EvidenceIDs     []uint       `json:"evidence_ids"`
	Reason          string       `json:"reason"`
	Summary         *string      `json:"summary,omitempty"`
	Upsert          []MemoryItem `json:"upsert,omitempty"`
	Forget          []string     `json:"forget,omitempty"`
}

func readGroupMemory(db *gorm.DB, groupID int64) (GroupProfile, error) {
	profile := GroupProfile{GroupID: groupID, Items: []MemoryItem{}}
	var row GroupMemory
	err := db.First(&row, "group_id = ?", groupID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return profile, nil
	}
	if err != nil {
		return profile, err
	}
	if err = json.Unmarshal([]byte(row.ProfileJSON), &profile); err != nil {
		return profile, err
	}
	profile.GroupID, profile.Version, profile.UpdatedAt = row.GroupID, row.Version, row.UpdatedAt
	return profile, nil
}
func writeGroupMemory(db *gorm.DB, profile GroupProfile) error {
	version := profile.Version
	profile.Version++
	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	row := GroupMemory{GroupID: profile.GroupID, ProfileJSON: string(data), Version: profile.Version, UpdatedAt: time.Now()}
	if version == 0 {
		return db.Create(&row).Error
	}
	result := db.Model(&GroupMemory{}).Where("group_id = ? AND version = ?", profile.GroupID, version).Updates(map[string]any{"profile_json": row.ProfileJSON, "version": row.Version, "updated_at": row.UpdatedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("group memory changed; read it again")
	}
	return nil
}
func MigrateGroupMemories(db *gorm.DB) error {
	var rows []GroupImpression
	return db.FindInBatches(&rows, 100, func(tx *gorm.DB, _ int) error {
		for _, old := range rows {
			profile := GroupProfile{GroupID: old.GroupID, Version: 1, Summary: old.Content, Items: []MemoryItem{}}
			data, err := json.Marshal(profile)
			if err != nil {
				return err
			}
			// Preserve full legacy text; automatic context reads remain bounded.
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&GroupMemory{GroupID: old.GroupID, Version: 1, ProfileJSON: string(data), UpdatedAt: time.Now()}).Error; err != nil {
				return err
			}
		}
		return nil
	}).Error
}
func visibleGroupProfile(profile GroupProfile, now time.Time, limit int) GroupProfile {
	items := make([]MemoryItem, 0, len(profile.Items))
	for _, item := range profile.Items {
		if item.GroupID == profile.GroupID {
			items = append(items, item)
		}
	}
	visible := visibleUserProfile(UserProfile{Summary: profile.Summary, Items: items}, profile.GroupID, now, limit)
	if limit < 30 {
		profile.Summary = visible.Summary
	}
	profile.Items = visible.Items
	return profile
}
func applyGroupMemoryChange(profile GroupProfile, change GroupMemoryChange, rows []ChatMessageRecord, selfID int64, now time.Time) (GroupProfile, error) {
	if profile.GroupID <= 0 || change.PreviousVersion != profile.Version {
		return profile, errors.New("invalid group or stale memory version")
	}
	if strings.TrimSpace(change.Reason) == "" || utf8.RuneCountInString(change.Reason) > 200 {
		return profile, errors.New("reason must contain 1 to 200 characters")
	}
	if len(change.EvidenceIDs) == 0 || len(change.EvidenceIDs) > 20 {
		return profile, errors.New("1 to 20 evidence_ids required")
	}
	evidence := map[uint]ChatMessageRecord{}
	for _, row := range rows {
		if row.GroupID == profile.GroupID && row.UserID > 0 && row.UserID != selfID && row.ID > profile.ManualThrough {
			evidence[row.ID] = row
		}
	}
	selected := map[uint]ChatMessageRecord{}
	for _, id := range change.EvidenceIDs {
		row, ok := evidence[id]
		if !ok {
			return profile, errors.New("group evidence must be new member messages from this group")
		}
		selected[id] = row
	}
	content, err := applyMemoryContent(UserProfile{Summary: profile.Summary, Items: profile.Items}, MemoryChange{Summary: change.Summary, Upsert: change.Upsert, Forget: change.Forget}, selected, profile.GroupID, now)
	if err != nil {
		return profile, err
	}
	profile.Summary, profile.Items = content.Summary, content.Items
	return profile, nil
}
func recordGroupMemoryAudit(db *gorm.DB, profile GroupProfile, change GroupMemoryChange) error {
	// UserID zero identifies a group-level audit.
	return recordMemoryAudit(db, profile.GroupID, UserProfile{Version: profile.Version}, MemoryChange{EvidenceIDs: change.EvidenceIDs, Reason: change.Reason})
}
