package api

import (
	"errors"
	"strings"

	consoleweb "github.com/berryhill/aegis/web/console"
)

// The query selects presentation from an already authenticated, exact-bound
// session projection only. It cannot select a subject, stanza or authority.
func selectConsoleAgentSession(detail *consoleweb.AgentDetailModel, id string) error {
	if id == "" {
		return nil
	}
	if detail == nil || len(id) > 256 || strings.ContainsAny(id, "\r\n\x00") {
		return errors.New("invalid Agent session selection")
	}
	for i := range detail.SessionRecords {
		if detail.SessionRecords[i].ID == id {
			detail.SelectedSession = &detail.SessionRecords[i]
			return nil
		}
	}
	return errors.New("Agent session evidence unavailable")
}
