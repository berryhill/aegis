package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

// Owning-service reviews are process-local, bounded, single-use and bound to
// authenticated transport identity. Restart loses reviews, never grants approval.
// Transport account identity is not an independent human decision. Typed reviews
// hand off to browser setup, which retains its session-bound CSRF/receipt boundary.
func registerDoerSetupServiceRoutes(g *echo.Group, svc *app.Service) {
	type review struct {
		Subject core.Subject
		Payload []byte
		Expires time.Time
	}
	var mu sync.Mutex
	reviews := map[string]review{}
	identity := func(s core.Subject) core.Subject {
		s.AuthenticatedAt = time.Time{}
		s.ExpiresAt = time.Time{}
		return s
	}
	g.POST("/loops/doer/drafts/:id/setup-review", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		var in app.DoerSetupReviewInput
		if err = decode(c, &in); err != nil {
			return err
		}
		if in.ID != c.Param("id") {
			return echo.NewHTTPError(http.StatusBadRequest, "draft path and body must match")
		}
		proposal, err := svc.ProposeDoerSetupSuccessorAs(c.Request().Context(), sub, in.ID, in.ExpectedVersion)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(proposal)
		if err != nil {
			return err
		}
		var entropy [32]byte
		if _, err = rand.Read(entropy[:]); err != nil {
			return err
		}
		token := hex.EncodeToString(entropy[:])
		now := svc.Now().UTC()
		expires := now.Add(5 * time.Minute)
		mu.Lock()
		for key, r := range reviews {
			if !now.Before(r.Expires) {
				delete(reviews, key)
			}
		}
		if len(reviews) >= 1024 {
			mu.Unlock()
			return echo.NewHTTPError(http.StatusServiceUnavailable, "setup review capacity exhausted")
		}
		reviews[token] = review{identity(sub), payload, expires}
		mu.Unlock()
		return c.JSON(http.StatusOK, app.DoerSetupReview{Proposal: proposal, Receipt: token, ExpiresAt: expires, ConfirmationURL: "/console/loops/doer/setup?draft_id=" + url.QueryEscape(in.ID)})
	})
	g.POST("/loops/doer/drafts/:id/setup-decision", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		var in app.DoerSetupDecisionInput
		if err = decode(c, &in); err != nil {
			return err
		}
		if in.ID != c.Param("id") || (in.Decision != "approve-successor" && in.Decision != "reject") {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid exact setup decision")
		}
		// No independently captured decision proof is supported by this typed
		// adapter. A model-held review token or SO_PEERCRED account identity
		// cannot authorize import, Agent approval or draft rebinding.
		if in.Decision == "approve-successor" {
			return echo.NewHTTPError(http.StatusForbidden, "independent human confirmation required: open the setup review confirmation_url; typed approval is unavailable")
		}
		mu.Lock()
		r, ok := reviews[in.Receipt]
		if ok && reflect.DeepEqual(r.Subject, identity(sub)) {
			delete(reviews, in.Receipt)
		} else {
			ok = false
		}
		mu.Unlock()
		if !ok || !svc.Now().Before(r.Expires) {
			return app.ErrDenied
		}
		var proposal app.DoerSetupProposal
		if err = json.Unmarshal(r.Payload, &proposal); err != nil {
			return err
		}
		if proposal.DraftID != in.ID {
			return app.ErrConflict
		}
		// Rejection reads the still-owned retained draft but applies no authority.
		if in.Decision == "reject" {
			d, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, in.ID)
			if err != nil {
				return err
			}
			return c.JSON(http.StatusOK, d)
		}
		return app.ErrDenied
	})
}
