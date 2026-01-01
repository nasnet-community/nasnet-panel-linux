package money

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Money is USD stored as integer cents. Keeps balances exact instead of
// drifting like float64 dollars do.
type Money int64

// FromDollars rounds a dollar amount to the nearest cent.
func FromDollars(d float64) Money { return Money(math.Round(d * 100)) }

// FromCents wraps a raw cent count.
func FromCents(c int64) Money { return Money(c) }

func (m Money) Cents() int64     { return int64(m) }
func (m Money) Dollars() float64 { return float64(m) / 100 }

// String renders dollars with two decimals, e.g. "4.99".
func (m Money) String() string { return strconv.FormatFloat(m.Dollars(), 'f', 2, 64) }

var ErrInvalid = errors.New("invalid money value")

// Parse reads user input like "4.99", "$5", "1,000". Rejects negatives and junk.
func Parse(s string) (Money, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrInvalid
	}
	d, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(d) || math.IsInf(d, 0) || d < 0 {
		return 0, ErrInvalid
	}
	return FromDollars(d), nil
}

// MarshalJSON emits a plain number with two decimals so the JSON API contract
// (dollars) stays unchanged even though we store cents.
func (m Money) MarshalJSON() ([]byte, error) { return []byte(m.String()), nil }

func (m *Money) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*m = 0
		return nil
	}
	d, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*m = FromDollars(d)
	return nil
}
