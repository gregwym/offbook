package model

import (
	"database/sql/driver"
	"encoding/json"

	"github.com/lib/pq"
)

// Tags is a transaction's user tag list (#544), stored as Postgres TEXT[].
// It never encodes as SQL NULL or JSON null: an untagged row is '{}' / [].
type Tags []string

// Value implements driver.Valuer.
func (t Tags) Value() (driver.Value, error) {
	if t == nil {
		return "{}", nil
	}
	return pq.StringArray(t).Value()
}

// Scan implements sql.Scanner.
func (t *Tags) Scan(src any) error {
	var a pq.StringArray
	if err := a.Scan(src); err != nil {
		return err
	}
	*t = Tags(a)
	if *t == nil {
		*t = Tags{}
	}
	return nil
}

// MarshalJSON keeps an untagged row as [] rather than null.
func (t Tags) MarshalJSON() ([]byte, error) {
	if t == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(t))
}
