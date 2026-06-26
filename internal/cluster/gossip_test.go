package cluster

import "testing"

func TestNormalizeAddr(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"http://Host:9300", "host:9300", true},
		{"http://host:9300/", "host:9300", true},
		{"https://host", "host:443", true},
		{"http://host", "host:80", true},
		{"  http://host:9300  ", "host:9300", true}, // trimmed
		{"https://heartd.lairner.com", "heartd.lairner.com:443", true},
		{"://nope", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeAddr(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("NormalizeAddr(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
