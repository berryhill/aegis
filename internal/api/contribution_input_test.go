package api

import (
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"net/http"
	"strings"
	"testing"
)

func TestContributionInputBoundSafeEnvelope(t *testing.T) {
	for _, bound := range []error{contribution.ErrSourceResponseBound, contribution.ErrSourceIssueBound, contribution.ErrSourceDiscussionBound} {
		status, code, message := classifyError(fmt.Errorf("private-content-canary: %w", bound))
		if status != http.StatusRequestEntityTooLarge || code != "contribution_source_bound" || message != bound.Error() {
			t.Fatal("unsafe source rejection", status, code, message)
		}
	}
	status, code, message := classifyError(fmt.Errorf("private-content-canary: %w", contribution.ErrInputBound))
	if status != http.StatusRequestEntityTooLarge || code != "contribution_input_bound" || message != contribution.ErrInputBound.Error() || strings.Contains(message, "canary") {
		t.Fatalf("unsafe bound diagnostic %d %s %s", status, code, message)
	}
}
