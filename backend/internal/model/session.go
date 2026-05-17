package model

import "time"

type Session struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	UserID     int64     `gorm:"not null" json:"user_id"`
	TokenHash  string    `gorm:"not null" json:"-"`
	ExpiresAt  time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

func (Session) TableName() string { return "sessions" }
