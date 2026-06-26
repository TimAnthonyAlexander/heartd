package cluster

import (
	"testing"

	"github.com/timanthonyalexander/heartd/internal/storage"
)

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

func TestShouldAddMember(t *testing.T) {
	self := "node-a"
	advertise := "http://node-a:9300"
	known := []storage.Peer{
		{Name: "node-b", URL: "http://node-b:9300"},
		{Name: "node-c", URL: "http://NODE-C:9300/"}, // cosmetic variant of below
	}

	tests := []struct {
		name   string
		member Member
		want   bool
	}{
		{"new node propagates", Member{Name: "node-d", URL: "http://node-d:9300"}, true},
		{"self by name is skipped", Member{Name: "node-a", URL: "http://elsewhere:9300"}, false},
		{"self by advertised address is skipped", Member{Name: "renamed-a", URL: "http://node-a:9300"}, false},
		{"already known by name is skipped", Member{Name: "node-b", URL: "http://node-b:9300"}, false},
		{"already known by address is skipped", Member{Name: "twin-c", URL: "http://node-c:9300"}, false},
		{"empty name is skipped", Member{Name: "", URL: "http://x:9300"}, false},
		{"empty url is skipped", Member{Name: "x", URL: ""}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAddMember(tc.member, self, advertise, known); got != tc.want {
				t.Errorf("shouldAddMember(%+v) = %v, want %v", tc.member, got, tc.want)
			}
		})
	}
}

// TestShouldAddMemberNoAdvertiseURL: a node with no advertise URL still can't add
// itself by name, but address-based self-detection is simply unavailable.
func TestShouldAddMemberNoAdvertiseURL(t *testing.T) {
	if shouldAddMember(Member{Name: "self", URL: "http://self:9300"}, "self", "", nil) {
		t.Error("a member matching self by name must be skipped even without an advertise URL")
	}
	if !shouldAddMember(Member{Name: "other", URL: "http://other:9300"}, "self", "", nil) {
		t.Error("a genuinely new member should be added even when self has no advertise URL")
	}
}
