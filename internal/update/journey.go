package update

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
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
	b, err := os.ReadFile(path)
	if err != nil {
		return CandidateIdentity{}, err
	}
	if err = validateCandidate(b, p.result.LatestVersion, runtime.GOOS, runtime.GOARCH); err != nil {
		return CandidateIdentity{}, err
	}
	info, err := buildinfo.ReadFile(path)
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
	sum := sha256.Sum256(b)
	if source == "" {
		return CandidateIdentity{}, errors.New("installed candidate source proof unavailable")
	}
	return CandidateIdentity{p.target, p.result.LatestVersion, source, hex.EncodeToString(sum[:])}, nil
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
	identity, err := p.Identity()
	if err != nil {
		return r, err
	}
	r = p.result
	r.ResumeRequired = true
	r.Phase = "preflight"
	if err = j.Preflight(ctx, identity); err != nil {
		return r, err
	}
	// A current healthy exact image requires neither auth nor a restart.
	if !r.UpdateAvailable && j.Healthy(ctx) {
		r.Phase = "verification"
		if err = j.Verify(ctx); err != nil {
			return r, err
		}
		if err = j.Record(ctx, "complete"); err != nil {
			return r, err
		}
		r.Phase = "complete"
		r.UpgradeComplete = true
		r.ResumeRequired = false
		return r, nil
	}
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"gateway_stop", j.Stop},
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
	r.Phase = "complete"
	if err = j.Record(ctx, "complete"); err != nil {
		return r, err
	}
	r.UpgradeComplete = true
	r.ResumeRequired = false
	r.GatewayImage = "current"
	r.RequiredAction = ""
	return r, nil
}
