package model

import (
	"time"

	"gorm.io/gorm"
)

type AIConversation struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	Title     *string        `json:"title,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Messages []AIMessage `gorm:"foreignKey:ConversationID" json:"-"`
}

func (AIConversation) TableName() string { return "ai_conversations" }
