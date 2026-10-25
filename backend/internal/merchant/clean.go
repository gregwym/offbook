// Package merchant derives a readable merchant label from a raw bank
// description (#588). Pure functions only: no DB, no I/O.
package merchant

import (
	"regexp"
	"strings"
	"unicode"
)

// processorPrefixes are payment-processor / card-network lead-ins banks put
// in front of the merchant ("SQ *BLUE BTL", "TST* JOE'S PIZZA"). Matched
// case-insensitively at the start, repeatedly, longest first.
var processorPrefixes = []string{
	"DEBIT CARD PURCHASE", "CHECK CARD PURCHASE", "POS PURCHASE", "POS DEBIT",
	"DEBIT PURCHASE", "CARD PURCHASE", "RECURRING PAYMENT", "ACH DEBIT", "ACH CREDIT",
	"CHECKCARD", "PAYPAL *", "PAYPAL*", "SQ *", "SQ*", "TST *", "TST*", "PP*",
	"POS ", "ACH ", "SP ", "SP*",
}

var (
	// trailingNoise: one trailing store/terminal/reference token — "#22",
	// "0412", "10/15", "10/15/26", "*1234", "x1234", "STORE 12".
	trailingNoise = regexp.MustCompile(`(?i)\s+(?:#\s*\d+|\*\d+|x\d{3,}|\d{1,2}/\d{1,2}(?:/\d{2,4})?|\d[\d-]*|[A-Za-z]{1,2}-\d{2,}|(?:STORE|STR|NO\.?)\s*#?\s*\d+)$`)
	spaces        = regexp.MustCompile(`\s+`)
)

// usStates are two-letter state codes a bank appends after the city. One
// is stripped only when at least two words remain before it, so "BP CA"
// keeps its code but "JOE'S PIZZA NY" loses it.
var usStates = map[string]bool{}

func init() {
	for _, s := range strings.Fields("AL AK AZ AR CA CO CT DE DC FL GA HI ID IL IN IA KS KY LA ME MD MA MI MN MS MO MT NE NV NH NJ NM NY NC ND OH OK OR PA RI SC SD TN TX UT VT VA WA WV WI WY") {
		usStates[s] = true
	}
}

// CleanDescription turns a raw bank string into a merchant label:
// processor prefixes and trailing store numbers, dates and an unambiguous
// state code go, whitespace collapses, and an all-caps result is
// title-cased. It returns raw (trimmed) when cleaning would leave nothing.
func CleanDescription(raw string) string {
	orig := strings.TrimSpace(spaces.ReplaceAllString(raw, " "))
	s := orig
	for changed := true; changed; {
		changed = false
		for _, p := range processorPrefixes {
			if len(s) > len(p) && strings.EqualFold(s[:len(p)], p) {
				s = strings.TrimSpace(s[len(p):])
				changed = true
				break
			}
		}
	}
	for {
		next := trailingNoise.ReplaceAllString(s, "")
		if f := strings.Fields(next); len(f) >= 3 && usStates[f[len(f)-1]] {
			next = strings.Join(f[:len(f)-1], " ")
		}
		next = strings.TrimRight(strings.TrimSpace(next), " *-#,")
		if next == s || next == "" {
			break
		}
		s = next
	}
	s = strings.Trim(s, " *-#,")
	if s == "" {
		return orig
	}
	if !hasLower(s) {
		s = titleCase(s)
	}
	return s
}

func hasLower(s string) bool {
	for _, r := range s {
		if unicode.IsLower(r) {
			return true
		}
	}
	return false
}

// titleCase lower-cases each word and capitalises its first letter
// ("NETFLIX.COM" -> "Netflix.com", "JOE'S" -> "Joe's"). Words with "&"
// stay upper ("AT&T", "H&M").
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if strings.Contains(w, "&") {
			continue
		}
		rs := []rune(strings.ToLower(w))
		for j, r := range rs {
			if unicode.IsLetter(r) {
				rs[j] = unicode.ToUpper(r)
				break
			}
		}
		words[i] = string(rs)
	}
	return strings.Join(words, " ")
}
