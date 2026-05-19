package model

import "time"

// UserSettings is per-user configuration for the AI advisor (and the
// scaffolding for any future per-user settings). One row per user; the
// settings service auto-creates the row on first read so handlers can
// treat absence as "all defaults" rather than handling a not-found path.
type UserSettings struct {
	UserID            int64     `gorm:"primaryKey" json:"user_id"`
	ClaudeAPIKeyEnc   []byte    `gorm:"column:claude_api_key_enc" json:"-"`
	OllamaBaseURL     *string   `gorm:"column:ollama_base_url" json:"ollama_base_url,omitempty"`
	PreferredProvider string    `gorm:"column:preferred_provider;not null;default:claude" json:"preferred_provider"`
	PreferredModel    *string   `gorm:"column:preferred_model" json:"preferred_model,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (UserSettings) TableName() string { return "user_settings" }
