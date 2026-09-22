package app

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestRuntimeSelectionHonorsExactCharterConstraint(t *testing.T) {
	for _, constraint := range []string{">=0.18.0,<0.19.0", core.HermesVersionConstraint} {
		for _, version := range []string{"0.18.2", "0.19.0", "0.21.3"} {
			t.Run(constraint+"/"+version, func(t *testing.T) {
				s := testService(t)
				ctx := context.Background()
				charter := testCharter(s.Now())
				charter.Runtime.VersionConstraint = constraint
				canonical, err := core.Canonicalize(charter)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Store.SaveCharter(canonical); err != nil {
					t.Fatal(err)
				}
				stored, err := s.GetCharter(charter.AgentID, charter.Revision)
				if err != nil {
					t.Fatal(err)
				}
				// Approve and provision on the originally compatible runtime before upgrade.
				review, err := s.PreviewPlan(ctx, charter.AgentID, charter.Revision, core.Environment{Name: "local"})
				if err != nil {
					t.Fatal(err)
				}
				approval, err := s.RequestApproval(ctx, review.Plan.ID, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				approval, err = s.DecideApproval(ctx, approval.ID, true)
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := s.Apply(ctx, review.Plan.ID, approval.ID)
				if err != nil {
					t.Fatal(err)
				}
				if receipt.Status != "verified" {
					t.Fatalf("receipt status = %s", receipt.Status)
				}
				// Exercise actual discovery and both charter-bound runtime selection paths.
				if err := os.WriteFile(s.Config.HermesExecutable, []byte("#!/bin/sh\nprintf 'Hermes Agent v"+version+"\\n'\n"), 0700); err != nil {
					t.Fatal(err)
				}
				denied := constraint == ">=0.18.0,<0.19.0" && version != "0.18.2"
				_, planErr := s.PreviewPlan(ctx, charter.AgentID, charter.Revision, core.Environment{Name: "local"})
				_, _, sessionErr := s.PreviewSession(ctx, charter.AgentID, charter.Revision, "principal", core.Environment{Name: "local"})
				for name, err := range map[string]error{"plan": planErr, "session": sessionErr} {
					if denied {
						if err == nil || !strings.Contains(err.Error(), "does not satisfy charter constraint") {
							t.Fatalf("%s must deny incompatible runtime, got %v", name, err)
						}
					} else if err != nil {
						t.Fatalf("%s rejected compatible runtime: %v", name, err)
					}
				}
				readback, err := s.GetCharter(charter.AgentID, charter.Revision)
				if err != nil {
					t.Fatal(err)
				}
				if readback.Digest != canonical.Digest || !bytes.Equal(readback.Canonical, stored.Canonical) {
					t.Fatal("runtime selection mutated persisted charter authority")
				}
			})
		}
	}
}
