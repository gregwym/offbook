package plaid_test

import (
	"testing"

	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
)

func TestMapAccountType(t *testing.T) {
	cases := []struct {
		name   string
		plaidT string
		plaidS string
		want   string
	}{
		{"depository/checking", "depository", "checking", "checking"},
		{"depository/savings", "depository", "savings", "savings"},
		{"depository/money_market", "depository", "money market", "savings"},
		{"depository/hsa", "depository", "hsa", "savings"},
		{"depository/cash_management", "depository", "cash management", "checking"},
		{"credit/credit_card", "credit", "credit card", "credit_card"},
		{"credit/paypal", "credit", "paypal", "credit_card"},
		{"investment/ira", "investment", "ira", "investment"},
		{"investment/_401k", "investment", "401k", "investment"},
		{"brokerage/brokerage", "brokerage", "brokerage", "investment"},
		{"loan/auto", "loan", "auto", "loan"},
		{"loan/student", "loan", "student", "loan"},
		{"depository/crypto_exchange", "depository", "crypto exchange", "crypto"},
		{"unknown_type_unknown_subtype", "weather", "thunderstorm", "other"},
		{"empty", "", "", "other"},
		{"case_insensitive_subtype", "DEPOSITORY", "CHECKING", "checking"},
		{"depository_no_subtype_falls_to_checking", "depository", "", "checking"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := plaidsvc.MapAccountType(tc.plaidT, tc.plaidS)
			if got != tc.want {
				t.Errorf("MapAccountType(%q, %q) = %q, want %q", tc.plaidT, tc.plaidS, got, tc.want)
			}
		})
	}
}
