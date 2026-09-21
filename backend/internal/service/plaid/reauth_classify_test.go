package plaid

import (
	"errors"
	"fmt"
	"testing"

	"github.com/plaid/plaid-go/v40/plaid"
)

// plaidAPIError builds the same shape SDK calls return on a Plaid API
// error: a plaid.GenericOpenAPIError whose Model() is a populated
// plaid.PlaidError. isReauthRequired unwraps to find exactly this shape.
func plaidAPIError(errorCode string) error {
	pe := plaid.PlaidError{
		ErrorType:      plaid.PLAIDERRORTYPE_ITEM_ERROR,
		ErrorCode:      errorCode,
		ErrorMessage:   "the login details of this item have changed",
		DisplayMessage: *plaid.NewNullableString(nil),
	}
	return plaid.MakeGenericOpenAPIError(nil, "boom", pe)
}

func TestIsReauthRequired(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ITEM_LOGIN_REQUIRED", plaidAPIError("ITEM_LOGIN_REQUIRED"), true},
		{"PENDING_EXPIRATION", plaidAPIError("PENDING_EXPIRATION"), true},
		{"generic ITEM_ERROR code", plaidAPIError("INTERNAL_SERVER_ERROR"), false},
		{"wrapped reauth error", fmt.Errorf("sync: %w", plaidAPIError("ITEM_LOGIN_REQUIRED")), true},
		{"non-Plaid error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isReauthRequired(tc.err); got != tc.want {
				t.Errorf("isReauthRequired(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
