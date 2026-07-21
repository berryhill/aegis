package manager

import "regexp"

type AuthorityReadIntent uint8

const (
	AuthorityReadUnknown AuthorityReadIntent = iota
	AuthorityReadCount
	AuthorityReadList
)

var (
	credentialObjectPattern = `(?:secrets?|credentials?|creds?|passwords?|passphrases?|tokens?|keys?)`
	credentialCountPattern  = regexp.MustCompile(`(?i)\b(?:how[	 ]*many|count|number[	 ]+of)\b[^\r\n]{0,80}\b` + credentialObjectPattern + `\b`)
	credentialListPattern   = regexp.MustCompile(`(?i)(?:\b(?:list|show)\b[^\r\n]{0,80}\b` + credentialObjectPattern + `\b|\bwhat\b[^\r\n]{0,80}\b` + credentialObjectPattern + `\b[^\r\n]{0,40}\b(?:have|stored)\b)`)
	credentialValueAction   = regexp.MustCompile(`(?i)\b(?:what[	 ]+is|show|give|retrieve|get|reveal|tell)\b`)
	credentialValueObject   = regexp.MustCompile(`(?i)\b(?:value|password|secret[	 ]+value|credential[	 ]+value)\b`)
	credentialNamedRef      = regexp.MustCompile(`(?i)\b(?:credential|secret|cred)[	 ]*(?::|name(?:d|s)?[	 ]+|called[	 ]+)[	 ]*(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))`)
	credentialValueRef      = regexp.MustCompile(`(?i)\b(?:value|password)[	 ]+(?:for|of)[	 ]+(?:the[	 ]+)?(?:credential|secret|cred)?[	 ]*:?[	 ]*(?:"([^"\r\n]{1,255})"|'([^'\r\n]{1,255})'|([a-z0-9][a-z0-9._/-]{0,254}))`)
)

func ParseAuthorityReadIntent(input string) AuthorityReadIntent {
	if credentialCountPattern.MatchString(input) {
		return AuthorityReadCount
	}
	if credentialListPattern.MatchString(input) {
		return AuthorityReadList
	}
	return AuthorityReadUnknown
}

func ParseCredentialValueReadIntent(input string) (string, bool) {
	if !credentialValueAction.MatchString(input) || !credentialValueObject.MatchString(input) {
		return "", false
	}
	for _, pattern := range []*regexp.Regexp{credentialNamedRef, credentialValueRef} {
		matches := pattern.FindAllStringSubmatch(input, -1)
		if len(matches) != 1 {
			continue
		}
		for _, candidate := range matches[0][1:] {
			if reference := identifierSlug(candidate); reference != "" {
				return reference, true
			}
		}
	}
	return "", false
}
