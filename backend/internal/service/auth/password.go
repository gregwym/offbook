package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is intentionally one notch above the default. ~250ms on a modern
// laptop — slow enough to frustrate offline cracking, fast enough that
// interactive signin still feels instant.
const bcryptCost = 12

var ErrPasswordMismatch = errors.New("password does not match")

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func CheckPassword(hash, plain string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}
		return err
	}
	return nil
}
