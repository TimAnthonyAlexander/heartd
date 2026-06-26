package storage

import "testing"

func TestCommonSecret(t *testing.T) {
	cases := []struct {
		name  string
		peers []Peer
		want  string
	}{
		{"none have a secret", []Peer{{Name: "a"}, {Name: "b"}}, ""},
		{"empty list", nil, ""},
		{"single shared secret (typical)", []Peer{{Secret: "s"}, {Secret: "s"}}, "s"},
		{"most common wins", []Peer{{Secret: "x"}, {Secret: "y"}, {Secret: "y"}}, "y"},
		{"empty secrets are ignored", []Peer{{Secret: ""}, {Secret: "z"}, {Secret: ""}}, "z"},
		{"tie breaks on smaller secret", []Peer{{Secret: "b"}, {Secret: "a"}}, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CommonSecret(c.peers); got != c.want {
				t.Errorf("CommonSecret = %q, want %q", got, c.want)
			}
		})
	}
}
