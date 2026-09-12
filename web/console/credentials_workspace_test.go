package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestCredentialWorkspaceStructureAndSafeDetail(t *testing.T) {
	record := RecordModel{Key: "secret-1", Label: "provider/test · active · v2", Revision: "v2", Lifecycle: "active", JSON: "RAW_DOSSIER_CANARY", Credential: &CredentialDetailModel{
		ID: "secret-1", Reference: "provider/test", Kind: "provider-token", Status: "active", CurrentVersion: 2,
		Versions: []CredentialVersionDetail{{Version: 1, CiphertextHash: "sha256:first"}, {Version: 2, CiphertextHash: "sha256:second"}},
		Backup:   CredentialBackupDetail{Available: true, TargetPath: "HOST_PATH_CANARY"},
		Proposal: CredentialProposalDetail{BackupCommand: "BACKUP_COMMAND_CANARY"},
	}}
	surface := SurfaceModel{Domain: DomainCredentials, Authoritative: true, State: "ready", Records: []RecordModel{record}, Inspector: &record, InspectorOpen: true, CollectionURL: "/console/credentials?q=provider&status=active#/credentials"}
	var out bytes.Buffer
	if err := CredentialWorkspace(surface).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	tables, rowHeaders, columnHeaders := 0, 0, 0
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "table" {
				tables++
			}
			for _, a := range n.Attr {
				if a.Key == "id" {
					if ids[a.Val] {
						t.Fatalf("duplicate ID %s", a.Val)
					}
					ids[a.Val] = true
				}
				if n.Data == "th" && a.Key == "scope" {
					if a.Val == "col" {
						columnHeaders++
					}
					if a.Val == "row" {
						rowHeaders++
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	if tables != 2 || columnHeaders != 7 || rowHeaders != 3 {
		t.Fatalf("tables=%d col headers=%d row headers=%d", tables, columnHeaders, rowHeaders)
	}
	for _, want := range []string{`data-pane="detail"`, `data-selected="true"`, `aria-current="true"`, `id="credential-inventory"`, `id="close-inspector"`, `record_key=secret-1#/credentials/secret-1`, `q=provider&amp;status=active#/credentials`, "Prepare rotation", "Prepare revoke", "Concealed · sha256:second", "current"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, no := range []string{"RAW_DOSSIER_CANARY", "HOST_PATH_CANARY", "BACKUP_COMMAND_CANARY", "Vault summary", "provider/test · active · v2"} {
		if strings.Contains(out.String(), no) {
			t.Errorf("leaked dossier field %q", no)
		}
	}
}

func TestCredentialWorkspaceDenialAndEmptyStates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		surface   SurfaceModel
		want      string
		forbidden string
	}{
		{"unavailable", SurfaceModel{ReasonCode: "custody_unavailable"}, "Count unavailable", "Prepare rotation"},
		{"empty", SurfaceModel{Authoritative: true}, "No credentials", "Prepare rotation"},
		{"filtered", SurfaceModel{Authoritative: true, State: "filtered-empty"}, "No matching credentials", "Prepare rotation"},
		{"missing", SurfaceModel{Authoritative: true, InspectorOpen: true}, "Credential unavailable", "Prepare rotation"},
		{"revoked", SurfaceModel{Authoritative: true, InspectorOpen: true, Inspector: &RecordModel{Key: "revoked", Lifecycle: "revoked", Credential: &CredentialDetailModel{ID: "revoked", Reference: "test"}}}, "Revoked credential", "Prepare rotation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := CredentialWorkspace(tc.surface).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) || strings.Contains(out.String(), tc.forbidden) {
				t.Fatalf("incorrect %s presentation", tc.name)
			}
			if tc.name == "revoked" {
				if !strings.Contains(out.String(), "record metadata and immutable version history remain inspectable here.") || strings.Contains(out.String(), "KEK metadata remain inspectable") {
					t.Fatal("revoked notice must describe the rendered metadata, not promise absent KEK inspection")
				}
			}
			if strings.Count(out.String(), `id="close-inspector"`) > 1 {
				t.Fatal("duplicate close control")
			}
		})
	}
}

func TestCredentialPaneAndResponsiveContract(t *testing.T) {
	for _, tc := range []struct {
		surface SurfaceModel
		pane    string
	}{{SurfaceModel{}, "list"}, {SurfaceModel{InspectorOpen: true}, "detail"}} {
		if got := credentialPane(tc.surface); got != tc.pane {
			t.Errorf("pane=%s want=%s", got, tc.pane)
		}
	}
	css := string(CSS)
	for _, want := range []string{"minmax(0,42fr) minmax(0,58fr)", ".credential-workspace>.inventory,.credential-workspace>.detail{overflow:auto", "@media(max-width:900px)", `.credential-workspace[data-pane="detail"]>.inventory{display:none}`, `.credential-workspace[data-pane="list"]>.detail{display:none}`} {
		if !strings.Contains(css, want) {
			t.Errorf("missing responsive rule %q", want)
		}
	}
	for _, want := range []string{"inventory.scrollTop", "event.persisted", "hashchange", "decodeURIComponent", "target.searchParams.set(\"record_key\", key)"} {
		if !strings.Contains(string(NavigationJS), want) {
			t.Errorf("missing navigation behavior %q", want)
		}
	}
}
