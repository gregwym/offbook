package categorization_test

import (
	"testing"

	"github.com/gregwym/offbook/backend/internal/service/categorization"
)

func TestNormalizeMerchantKey(t *testing.T) {
	tests := []struct {
		name             string
		merchantName     *string
		descriptionClean *string
		want             string
	}{
		{"merchant name preferred", ptr("  whole foods  "), ptr("WFM #123"), "WHOLE FOODS"},
		{"falls back to description_clean", nil, ptr("amzn mktp us"), "AMZN MKTP US"},
		{"merchant name empty falls back", ptr("   "), ptr("Trader Joes"), "TRADER JOES"},
		{"both nil", nil, nil, ""},
		{"both empty", ptr(""), ptr(""), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := categorization.NormalizeMerchantKey(tt.merchantName, tt.descriptionClean)
			if got != tt.want {
				t.Errorf("NormalizeMerchantKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
