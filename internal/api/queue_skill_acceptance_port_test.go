package api

import "testing"

func TestQueueSkillAcceptanceAddress(t *testing.T) {
	for _, tc := range []struct {
		port, want string
		valid      bool
	}{
		{"", "127.0.0.1:0", true}, {"8450", "127.0.0.1:8450", true}, {"8469", "127.0.0.1:8469", true},
		{"8448", "", false}, {"8098", "", false}, {"8470", "", false}, {"08450", "", false}, {"localhost:8450", "", false},
	} {
		got, err := queueSkillAcceptanceAddress(tc.port)
		if (err == nil) != tc.valid || got != tc.want {
			t.Errorf("port %q: got %q %v", tc.port, got, err)
		}
	}
}
