package contribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

const Schema = "aegis.contribution.intake.v1"

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidKey(k string) bool { return keyPattern.MatchString(k) }

// Contract is sealed at authenticated admission, not constructed by a model.
// It binds one issue writer to the existing Queue, not a second execution queue.
type Contract struct {
	SchemaVersion   string                `json:"schema_version"`
	OwnerID         string                `json:"owner_id"`
	SessionID       string                `json:"session_id"`
	Key             string                `json:"key"`
	PayloadDigest   string                `json:"payload_digest"`
	QueueItemID     string                `json:"queue_item_id"`
	SubmissionID    string                `json:"submission_id"`
	RunID           string                `json:"run_id"`
	Loop            reference.RevisionRef `json:"loop"`
	Controller      reference.RevisionRef `json:"controller"`
	Builder         reference.RevisionRef `json:"builder"`
	Tester          reference.RevisionRef `json:"tester"`
	Source          Source                `json:"source"`
	NarrowerScope   string                `json:"narrower_scope"`
	Outcome         string                `json:"outcome"`
	Acceptance      string                `json:"acceptance"`
	Invariants      []string              `json:"invariants"`
	Exclusions      []string              `json:"exclusions"`
	Repository      string                `json:"repository"`
	Base            string                `json:"base"`
	Worktree        string                `json:"worktree"`
	Effect          string                `json:"effect"`
	Roles           []RoleBinding         `json:"roles"`
	TemplateDigest  string                `json:"template_digest"`
	CandidateBudget uint8                 `json:"candidate_budget"`
	AdmittedAt      time.Time             `json:"admitted_at"`
	Deadline        time.Time             `json:"deadline"`
	Digest          string                `json:"digest"`
}

func (c Contract) validate() error {
	if c.SchemaVersion != Schema || c.OwnerID == "" || c.SessionID == "" || !ValidKey(c.Key) || c.QueueItemID == "" || c.SubmissionID == "" || c.RunID == "" || c.PayloadDigest == "" || c.Source.RepositoryID == 0 || c.Source.IssueID == 0 || c.Source.ContentDigest == "" || c.Outcome == "" || c.Acceptance == "" || c.TemplateDigest == "" || c.CandidateBudget < 1 || c.CandidateBudget > 2 || !ValidDelivery(c.Effect) || c.Worktree == "" || c.Base == "" || c.Repository == "" || c.AdmittedAt.IsZero() || !c.Deadline.After(c.AdmittedAt) || c.Deadline.Sub(c.AdmittedAt) > 24*time.Hour || len(c.NarrowerScope) > 4096 {
		return errors.New("invalid contribution contract")
	}
	for _, ref := range []reference.RevisionRef{c.Loop, c.Controller, c.Builder, c.Tester} {
		if ref.Validate() != nil {
			return errors.New("exact contribution references required")
		}
	}
	if len(c.Roles) != 3 || c.Invariants == nil || c.Exclusions == nil || c.Source.Discussion == nil || c.Source.DiscussionTruncated || len(c.Source.Discussion) > MaxDiscussion || c.Source.Author.ID == 0 || c.Source.Author.Login == "" || c.Source.State != "open" || c.Source.CreatedAt.IsZero() || c.Source.ObservedAt.IsZero() || c.Source.UpdatedAt.IsZero() || len(c.Source.Title) > 1024 || len(c.Source.Body) > 65536 || len(c.Outcome) > 16384 || len(c.Acceptance) > 16384 || len(c.Base) > 255 || len(c.Worktree) > 4096 {
		return errors.New("invalid contribution metadata or bounds")
	}
	for i, ref := range []reference.RevisionRef{c.Controller, c.Builder, c.Tester} {
		role := c.Roles[i]
		if role.Agent != ref || role.Charter.Validate() != nil || role.Runtime.Validate() != nil {
			return errors.New("exact immutable role bindings required")
		}
	}
	for _, digest := range []string{c.PayloadDigest, c.TemplateDigest, c.Source.ContentDigest} {
		if (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "contribution", Digest: digest}).Validate() != nil {
			return errors.New("invalid contribution digest")
		}
	}
	for _, comment := range c.Source.Discussion {
		if comment.ID == 0 || comment.Author.ID == 0 || comment.Author.Login == "" || comment.CreatedAt.IsZero() || comment.UpdatedAt.IsZero() || len(comment.Body) > 32768 || (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "comment", Digest: comment.ContentDigest}).Validate() != nil {
			return errors.New("invalid discussion provenance")
		}
	}
	parsed, err := ParseIssueURL(c.Source.URL.URL)
	if err != nil || parsed != c.Source.URL || c.Repository != parsed.Owner+"/"+parsed.Repository {
		return errors.New("contribution source mismatch")
	}
	return nil
}
func Seal(c Contract) (Contract, error) {
	c.Digest = ""
	if c.Effect == "" {
		c.Effect = string(DeliveryMerge)
	}
	if err := c.validate(); err != nil {
		return Contract{}, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return Contract{}, err
	}
	c.Digest = Digest(b)
	return c, nil
}
func Marshal(c Contract) ([]byte, error) {
	sealed, err := Seal(c)
	if err != nil || sealed.Digest != c.Digest {
		return nil, errors.New("contribution digest mismatch")
	}
	return json.Marshal(c)
}
func Unmarshal(b []byte) (Contract, error) {
	var c Contract
	if err := reference.RejectDuplicateObjectKeys(b); err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if d.Decode(new(any)) != io.EOF {
		return c, errors.New("trailing contribution data")
	}
	_, err := Marshal(c)
	return c, err
}
func Payload(loop reference.RevisionRef, url, scope string) string {
	b, _ := json.Marshal(struct {
		Loop       reference.RevisionRef
		URL, Scope string
	}{loop, url, scope})
	return Digest(b)
}
