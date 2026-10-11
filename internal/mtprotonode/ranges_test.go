package mtprotonode

import "testing"

func TestDomesticIsFalseWhenAnyAddressIsOutside(t *testing.T) {
	r := PrefixRanges([]string{"192.0.2.0/24", "not a prefix"}, []string{"2001:db8::/32"})
	cases := []struct {
		addrs []string
		want  *bool
	}{
		{[]string{"192.0.2.10"}, ptr(true)},
		{[]string{"192.0.2.10", "2001:db8::1", "::ffff:192.0.2.11"}, ptr(true)},
		{[]string{"192.0.2.10", "198.51.100.7"}, ptr(false)},
		{[]string{"garbage", "198.51.100.7"}, ptr(false)},
		{[]string{"garbage"}, nil},
		{nil, nil},
	}
	for _, c := range cases {
		got := domestic(r, c.addrs)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%v: got %v, want %v", c.addrs, show(got), show(c.want))
		}
	}
	// no list (a build without it): never a warning
	if got := domestic(nil, []string{"198.51.100.7"}); got != nil {
		t.Errorf("no ranges: got %v", *got)
	}
	if PrefixRanges(nil, []string{"junk"}) != nil {
		t.Error("an empty list must be no list, or everything would look outside")
	}
}

func ptr(b bool) *bool { return &b }

func show(b *bool) any {
	if b == nil {
		return "nil"
	}
	return *b
}
