package model

import (
	"time"

	"gorm.io/gorm"
)

// Scope keys persisted on User and surfaced via /me/scope.
const (
	ScopePersonal  = "personal"
	ScopeHousehold = "household"
)

type User struct {
	ID           int64          `gorm:"primaryKey" json:"id"`
	Email        string         `gorm:"not null" json:"email"`
	PasswordHash string         `gorm:"not null" json:"-"`
	IsAdmin      bool           `gorm:"not null;default:false" json:"is_admin"`
	LastScope    string         `gorm:"not null;default:personal" json:"last_scope"`
	DefaultScope string         `gorm:"not null;default:personal" json:"default_scope"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }
