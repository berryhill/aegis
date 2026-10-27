package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestContributionDeliveryPublicLoader(t *testing.T) {
	valid := &ContributionDelivery{Convention: "contribution-publication.v1", Actor: "controller", Author: ContributionGitIdentity{Name: "Author", Email: "author@example.invalid"}, Committer: ContributionGitIdentity{Name: "Committer", Email: "committer@example.invalid"}, RequiredChecks: []string{"ci"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"postmerge"}, MergeMethod: "squash", ProtectionPolicy: "exact-protection"}
	for _, name := range []string{"valid", "absent", "unknown-subfield", "invalid-policy"} {
		t.Run(name, func(t *testing.T) {
			cfg := Defaults()
			cfg.Principal = Principal{ID: "principal", Name: "Principal", UID: "4242", User: "operator", AuthTTL: cfg.Principal.AuthTTL}
			if name != "absent" {
				p := *valid
				cfg.ContributionDelivery = &p
			}
			if name == "invalid-policy" {
				cfg.ContributionDelivery.MergeMethod = "unchecked"
			}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if name == "unknown-subfield" {
				var doc map[string]any
				if err = json.Unmarshal(raw, &doc); err != nil {
					t.Fatal(err)
				}
				doc["contribution_delivery"].(map[string]any)["authenticate_from_claim"] = true
				raw, err = json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "aegis.json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path, nil)
			if name == "unknown-subfield" || name == "invalid-policy" {
				if err == nil {
					t.Fatal("unsafe policy accepted by public loader")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.ContributionDelivery, cfg.ContributionDelivery) {
				t.Fatalf("roundtrip mismatch: %#v", got.ContributionDelivery)
			}
		})
	}
}
