package implementation

import "strings"

func containsPrivateKeyHeading(b []byte) bool {
	for _, kind := range []string{"", "RSA ", "EC ", "DSA ", "OPENSSH ", "ENCRYPTED "} {
		if strings.Contains(string(b), "-----BEGIN "+kind+"PRIVATE "+"KEY-----") {
			return true
		}
	}
	return false
}
