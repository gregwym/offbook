package model

import (
	"encoding/json"
	"time"
)

type AIMessage struct {
	ID       int64 `gorm:"primaryKey" json:"id"`
	ThreadID int64 `gorm:"column:thread_id;not null" json:"thread_id"`
	// UserID identifies who posted a `user`-role turn. Nullable: assistant
	// turns leave it NULL, and turns recorded before migration 000011
	// existed are also NULL. Shared threads (`shared_with_household=true`)
	// rely on this column to attribute messages across members.
	UserID          *int64          `gorm:"column:user_id" json:"user_id,omitempty"`
	Role            string          `gorm:"not null" json:"role"`
	Content         string          `gorm:"not null" json:"content"`
	ContextSnapshot json.RawMessage `gorm:"type:jsonb" json:"context_snapshot,omitempty"`
	Provider        *string         `json:"provider,omitempty"`
	ModelName       *string         `json:"model_name,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

func (AIMessage) TableName() string { return "ai_messages" }
