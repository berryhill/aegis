package contribution

import "testing"

func TestIssueURLIsClosedAndCanonical(t *testing.T) {
	good := "https://github.com/berryhill/aegis/issues/375"
	u, err := ParseIssueURL(good)
	if err != nil || u.Owner != "berryhill" || u.Repository != "aegis" || u.Number != 375 || u.URL != good {
		t.Fatalf("unexpected issue: %+v %v", u, err)
	}
	for _, bad := range []string{"http://github.com/berryhill/aegis/issues/375", "https://github.com.evil.test/berryhill/aegis/issues/375", "https://user@github.com/berryhill/aegis/issues/375", "https://github.com:443/berryhill/aegis/issues/375", "https://github.com/berryhill/aegis/pull/375", good + "?x=1", good + "#comment", good + "/", "https://github.com/berryhill/aegis/issues/0375", "https://github.com/berryhill/aegis/issues/0", "https://github.com/berryhill/%61egis/issues/375", "https://github.com/berryhill/../issues/375"} {
		if _, err := ParseIssueURL(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
