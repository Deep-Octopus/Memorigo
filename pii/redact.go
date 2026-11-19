// Package pii redacts a few high-confidence patterns. It is opt-in and it is
// not a general anonymizer: names, addresses and free-form identifiers stay.
package pii

import (
	"regexp"
	"strings"
)

const (
	EmailToken = "[redacted:email]"
	PhoneToken = "[redacted:phone]"
	CardToken  = "[redacted:card]"
)

var (
	emailRE = regexp.MustCompile(`(?i)[a-z0-9][a-z0-9._%+\-]{0,64}@[a-z0-9][a-z0-9.\-]{0,253}\.[a-z]{2,24}`)
	// Compact international numbers, including a leading +.
	e164RE = regexp.MustCompile(`(?:^|[^\d])(\+\d{8,15})(?:$|[^\d])`)
	// Mainland China mobile numbers.
	cnMobileRE = regexp.MustCompile(`(?:^|[^\d])(1[3-9]\d{9})(?:$|[^\d])`)
	// NANP numbers with separators, optional leading country code.
	nanpRE = regexp.MustCompile(`(?:^|[^\d])((?:\+\d{1,3}[\s.\-]?)?\(?\d{3}\)?[\s.\-]\d{3}[\s.\-]\d{4})(?:$|[^\d])`)
)

// Redact replaces emails, phone numbers and Luhn-valid card numbers.
// Running it twice is a no-op on the tokens it inserts.
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = emailRE.ReplaceAllString(s, EmailToken)
	s = redactCards(s)
	s = replaceGroup(s, e164RE, PhoneToken)
	s = replaceGroup(s, cnMobileRE, PhoneToken)
	s = replaceGroup(s, nanpRE, PhoneToken)
	return s
}

func replaceGroup(s string, re *regexp.Regexp, token string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		start, end := m[2], m[3]
		if start < 0 || end < start || start < last {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(token)
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// redactCards replaces 13–19 digit runs, separators allowed, when they pass Luhn.
func redactCards(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] < '0' || s[i] > '9' || (i > 0 && s[i-1] >= '0' && s[i-1] <= '9') {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i
		digits := make([]byte, 0, 19)
		lastSep := false
		for j < len(s) {
			c := s[j]
			if c >= '0' && c <= '9' {
				if len(digits) == 19 {
					break
				}
				digits = append(digits, c)
				lastSep = false
				j++
				continue
			}
			if (c == ' ' || c == '-') && !lastSep && j+1 < len(s) && s[j+1] >= '0' && s[j+1] <= '9' {
				lastSep = true
				j++
				continue
			}
			break
		}
		followedByDigit := j < len(s) && s[j] >= '0' && s[j] <= '9'
		if !followedByDigit && len(digits) >= 13 && len(digits) <= 19 && luhn(digits) {
			b.WriteString(CardToken)
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func luhn(digits []byte) bool {
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0 && sum > 0
}
