package core

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func fuzzCharter() Charter {
	return Charter{SchemaVersion: SchemaVersion, AgentID: "fuzz-agent", Name: "Fuzz", Revision: 1, Runtime: RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0,<0.19.0", Target: "ephemeral"}, Stanzas: []TrustStanza{{ID: "principal", Name: "principal", Enabled: true, Authentication: AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []IdentitySelector{{PrincipalIDs: []string{"principal-1"}}}}, Scopes: Scopes{Credentials: []string{"provider:test"}}, Session: SessionPolicy{MaximumLifetimeSec: 60}, Approval: ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: InformationFlowPolicy{CrossStanza: "deny"}, Hermes: HermesConfig{Model: "test", Provider: "test"}}}, CreatedBy: "principal-1", CreatedAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)}
}
func FuzzCharterDecoding(f *testing.F) {
	b, _ := json.Marshal(fuzzCharter())
	f.Add(b)
	f.Add([]byte(`{"unknown":true}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		c, err := DecodeCharter(bytes.NewReader(b))
		if err == nil {
			if ValidateCharter(c) != nil {
				t.Fatal("decoder accepted invalid charter")
			}
		}
	})
}
func FuzzCanonicalization(f *testing.F) {
	f.Add("name")
	f.Fuzz(func(t *testing.T, name string) {
		c := fuzzCharter()
		c.Name = name
		a, err := Canonicalize(c)
		if err != nil {
			return
		}
		b, err := Canonicalize(c)
		if err != nil {
			t.Fatal(err)
		}
		if !EqualCanonical(a, b) {
			t.Fatal("canonicalization is nondeterministic")
		}
	})
}
func FuzzApprovalPayloadBinding(f *testing.F) {
	f.Add("charter-digest", "runtime", "environment", "target")
	f.Fuzz(func(t *testing.T, charterDigest, runtime, environment, target string) {
		plan := Plan{ID: "plan", AgentID: "agent", Revision: 1, CharterDigest: charterDigest, Runtime: RuntimeDescriptor{Runtime: runtime, Version: "0.18.2"}, Environment: Environment{Name: environment}, Effects: []Effect{{Kind: EffectCreateFile, Target: target, Digest: "effect"}}, CreatedAt: time.Unix(1, 0).UTC()}
		digest := PlanDigest(plan)
		plan.Effects[0].Target += "x"
		if PlanDigest(plan) == digest {
			t.Fatal("effect mutation did not alter plan digest")
		}
		plan.Effects[0].Target = target
		plan.Environment.Name += "x"
		if PlanDigest(plan) == digest {
			t.Fatal("environment mutation did not alter plan digest")
		}
	})
}

func FuzzSelectorOverlapSymmetry(f *testing.F) {
	f.Add("human", "human", "principal-1", "principal-1", "local-os", "local-os")
	f.Add("human", "service", "", "", "local-os", "mtls")
	f.Fuzz(func(t *testing.T, ak, bk, aid, bid, am, bm string) {
		a := IdentitySelector{Kinds: []string{ak}, SubjectIDs: nonEmpty(aid)}
		b := IdentitySelector{Kinds: []string{bk}, SubjectIDs: nonEmpty(bid)}
		ab := selectorsOverlap([]string{am}, a, []string{bm}, b)
		ba := selectorsOverlap([]string{bm}, b, []string{am}, a)
		if ab != ba {
			t.Fatal("selector overlap is not symmetric")
		}
	})
}

func nonEmpty(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}
