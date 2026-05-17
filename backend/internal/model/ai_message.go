package model

import (
	"encoding/json"
	"time"
)

type AIMessage struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	ThreadID        int64           `gorm:"column:thread_id;not null" json:"thread_id"`
	Role            string          `gorm:"not null" json:"role"`
	Content         string          `gorm:"not null" json:"content"`
	ContextSnapshot json.RawMessage `gorm:"type:jsonb" json:"context_snapshot,omitempty"`
	Provider        *string         `json:"provider,omitempty"`
	ModelName       *string         `json:"model_name,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

func (AIMessage) TableName() string { return "ai_messages" }
