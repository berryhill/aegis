package manager

import "testing"

func TestParseAuthorityReadIntent(t *testing.T) {
	for _, test := range []struct {
		input string
		want  AuthorityReadIntent
	}{
		{input: "how many secrets do we have?", want: AuthorityReadCount},
		{input: "howmany creds are stored?", want: AuthorityReadCount},
		{input: "give me the count of credentials", want: AuthorityReadCount},
		{input: "list my secrets", want: AuthorityReadList},
		{input: "show all credentials", want: AuthorityReadList},
		{input: "what secrets do we have?", want: AuthorityReadList},
		{input: "explain how credentials work", want: AuthorityReadUnknown},
		{input: "hello", want: AuthorityReadUnknown},
	} {
		if got := ParseAuthorityReadIntent(test.input); got != test.want {
			t.Errorf("ParseAuthorityReadIntent(%q)=%v want %v", test.input, got, test.want)
		}
	}
}

func TestParseCredentialValueReadIntent(t *testing.T) {
	for _, test := range []struct {
		input     string
		reference string
		ok        bool
	}{
		{input: `what is the secret value for test?`, reference: "test", ok: true},
		{input: `what is the value for the credential: "test"`, reference: "test", ok: true},
		{input: `show me the password for credential named github`, reference: "github", ok: true},
		{input: `what credentials do we have?`},
		{input: `explain secret values`},
	} {
		reference, ok := ParseCredentialValueReadIntent(test.input)
		if ok != test.ok || reference != test.reference {
			t.Errorf("ParseCredentialValueReadIntent(%q)=(%q,%t) want (%q,%t)", test.input, reference, ok, test.reference, test.ok)
		}
	}
}
