package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
)

// CandidateIdentity is derived from the sealed image, never from a journal.
type CandidateIdentity struct{ Executable, Version, Source, Digest string }

func (p *Prepared) Identity() (CandidateIdentity, error) {
	path := p.candidate
	if path == "" {
		path = p.target
	}
	f, err := os.Open(path)
	if err != nil {
		return CandidateIdentity{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxBinarySize+1))
	if err != nil || len(b) > maxBinarySize {
		return CandidateIdentity{}, errors.New("candidate identity bytes unavailable or oversized")
	}
	expected := p.candidateDigest
	if p.candidate == "" {
		expected = p.targetDigest
	}
	identityDigest := sha256.Sum256(b)
	if identityDigest != expected {
		return CandidateIdentity{}, errors.New("candidate identity differs from prepared bytes")
	}
	if err = validateCandidate(b, p.result.LatestVersion, runtime.GOOS, runtime.GOARCH); err != nil {
		return CandidateIdentity{}, err
	}
	info, err := buildinfo.Read(bytes.NewReader(b))
	if err != nil {
		return CandidateIdentity{}, err
	}
	var source string
	for _, s := range info.Settings {
		if s.Key == "-ldflags" {
			for _, f := range strings.Fields(s.Value) {
				if v, ok := strings.CutPrefix(f, "github.com/berryhill/aegis/internal/buildinfo.SourceRevision="); ok {
					source = v
				}
			}
		}
	}
	if source == "" {
		return CandidateIdentity{}, errors.New("installed candidate source proof unavailable")
	}
	return CandidateIdentity{p.target, p.result.LatestVersion, source, hex.EncodeToString(identityDigest[:])}, nil
}

// Journey is the production application boundary. Preparation always precedes
// authentication/stop. Resume callbacks must reobserve reality rather than skip
// work based on recorded phases. No automatic binary rollback is attempted.
type Journey interface {
	Preflight(context.Context, CandidateIdentity) error
	Healthy(context.Context) bool
	Stop(context.Context) error
	Recover(context.Context) error
	Activate(context.Context) error
	Verify(context.Context) error
	Record(context.Context, string) error
}

func RunJourney(ctx context.Context, binary BinaryUpdater, j Journey) (r Result, err error) {
	r = Result{Phase: "preflight", ResumeRequired: true}
	defer func() {
		if err != nil {
			err = errors.Join(errors.New("upgrade incomplete at "+r.Phase), err)
		}
	}()
	preparer, ok := binary.(interface {
		Prepare(context.Context) (*Prepared, error)
	})
	if !ok {
		return r, errors.New("verified candidate preparation unavailable")
	}
	r.Phase = "candidate_prepare"
	p, err := preparer.Prepare(ctx)
	if err != nil {
		return r, err
	}
	defer p.Close()
	if receiver, ok := j.(interface {
		BindPreparedValidation(func(context.Context) error)
	}); ok {
		receiver.BindPreparedValidation(p.Validate)
	}
	identity, err := p.Identity()
	if err != nil {
		return r, err
	}
	complete := func() Result {
		r.Phase = "complete"
		r.UpgradeComplete = true
		r.ResumeRequired = false
		r.RunningVersion = identity.Version
		r.RunningSourceRevision = identity.Source
		r.GatewayImage = "running_current_image"
		r.GatewayRestartRequired = false
		r.AuditCurrent = true
		r.ConsoleVerified = true
		r.RequiredAction = ""
		if report, ok := j.(interface{ VerifiedConsoleURL() string }); ok {
			r.ConsoleURL = report.VerifiedConsoleURL()
		}
		return r
	}
	r = p.result
	r.ResumeRequired = true
	r.Phase = "preflight"
	if err = j.Preflight(ctx, identity); err != nil {
		return r, err
	}
	// A current healthy exact image needs no lifecycle mutation or restart.
	// Verification may still require protected authentication for Console data.
	if !r.UpdateAvailable && j.Healthy(ctx) {
		r.Phase = "verification"
		if err = j.Verify(ctx); err != nil {
			return r, err
		}
		if err = j.Record(ctx, "complete"); err != nil {
			return r, err
		}
		return complete(), nil
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"gateway_stop", func(ctx context.Context) error {
			// Protected input may take time. Recheck before interrupting a gateway.
			if err := p.Validate(ctx); err != nil {
				return err
			}
			return j.Stop(ctx)
		}},
		{"binary_install", func(ctx context.Context) error {
			if e := p.Validate(ctx); e != nil {
				return e
			}
			var e error
			r, e = p.Install(ctx)
			r.Phase = "binary_install"
			r.ResumeRequired = true
			return e
		}},
		{"authority_validation", j.Recover}, {"gateway_activation", j.Activate}, {"verification", j.Verify},
	} {
		r.Phase = step.name
		if err = ctx.Err(); err != nil {
			return r, err
		}
		if err = j.Record(ctx, step.name); err != nil {
			return r, err
		}
		if err = step.run(ctx); err != nil {
			return r, err
		}
	}
	r.Phase = "completion_record"
	if err = j.Record(ctx, "complete"); err != nil {
		return r, err
	}
	return complete(), nil
}
