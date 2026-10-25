package merchant

import "testing"

func TestCleanDescription_Table(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SQ *BLUE BTL 0412", "Blue Btl"},
		{"SQ *BLUE BTL 0415", "Blue Btl"},
		{"NETFLIX.COM 0915", "Netflix.com"},
		{"NETFLIX.COM 1015", "Netflix.com"},
		{"TST* JOE'S PIZZA #22", "Joe's Pizza"},
		{"TST*JOE'S PIZZA NEW YORK NY", "Joe's Pizza New York"},
		{"PAYPAL *SPOTIFY", "Spotify"},
		{"SP * ALLBIRDS", "Allbirds"},
		{"POS SHELL OIL 57444", "Shell Oil"},
		{"DEBIT CARD PURCHASE STARBUCKS STORE 1234", "Starbucks"},
		{"WHOLEFDS MKT 10/15", "Wholefds Mkt"},
		{"TARGET T-1234 10/15/26", "Target"},
		{"ACH ELECTRIC CO 991-22", "Electric Co"},
		{"AMAZON MKTPL*1A2B3C", "Amazon Mktpl*1a2b3c"},
		{"AT&T BILL PAYMENT", "AT&T Bill Payment"},
		{"BP CA", "Bp Ca"},
		{"Blue Bottle Coffee", "Blue Bottle Coffee"},
		{"  UBER   TRIP  ", "Uber Trip"},
		{"12345", "12345"},
		{"SQ *", "Sq"},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := CleanDescription(c.in); got != c.want {
				t.Errorf("CleanDescription(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
