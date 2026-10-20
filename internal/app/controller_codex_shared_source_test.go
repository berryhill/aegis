package app

import "testing"

func TestControllerCodexExplicitSharedSource(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/home/operator/.hermes", true},
		{"/home/operator/.hermes/profiles/xander", true},
		{"/home/another/.hermes", false},
		{"/home/operator/.hermes/other", false},
		{"/home/operator/.hermes/profiles/xander/../another", false},
		{".hermes", false},
	} {
		if got := nativeControllerSourceAllowed(tc.path, "/home/operator"); got != tc.want {
			t.Errorf("source %q allowed=%v, want %v", tc.path, got, tc.want)
		}
	}
}
