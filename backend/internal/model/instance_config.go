package model

import "time"

// Signup modes set during the first-boot wizard.
const (
	SignupModeLocalMultiTenant = "local_multi_tenant"
	SignupModeInviteOnly       = "invite_only"
)

// InstanceConfig is a singleton (id=1). Set once during /setup/admin.
type InstanceConfig struct {
	ID         int16     `gorm:"primaryKey" json:"id"`
	SignupMode string    `gorm:"not null" json:"signup_mode"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (InstanceConfig) TableName() string { return "instance_config" }
