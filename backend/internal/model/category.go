package model

import (
	"time"

	"gorm.io/gorm"
)

type Category struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	// UserID owns a user-created category; NULL means a system category
	// (the seeded taxonomy). Reads are scoped to "user_id IS NULL OR = me".
	// Intentionally no DB FK to users yet — see migration 000017 (added with
	// category CRUD to avoid TRUNCATE-CASCADE wiping seeded categories).
	UserID    *int64         `json:"user_id,omitempty"`
	ParentID  *int64         `json:"parent_id,omitempty"`
	Name      string         `gorm:"not null" json:"name"`
	Slug      string         `gorm:"not null" json:"slug"`
	Icon      *string        `json:"icon,omitempty"`
	Color     *string        `json:"color,omitempty"`
	IsSystem  bool           `gorm:"not null;default:false" json:"is_system"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Parent   *Category  `gorm:"foreignKey:ParentID" json:"-"`
	Children []Category `gorm:"foreignKey:ParentID" json:"-"`
}

func (Category) TableName() string { return "categories" }
