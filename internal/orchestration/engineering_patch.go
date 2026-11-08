package orchestration

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
	"regexp"
)

const engineeringHunkInstruction = " Engineering text assets also support exact byte hunks: {\"path\":\"README.md\",\"preimage_digest\":\"sha256:... full file digest\",\"preimage_mode\":420,\"hunks\":[{\"start\":0,\"end\":0,\"content\":\"base64 replacement bytes\"}]}. Omit whole-file content for hunks. Ordered disjoint byte ranges apply only against the complete exact preimage and unchanged mode; at most 64 hunks and 65536 replacement bytes. No fuzzy application, commands or authority."

func decodeEngineeringPatch(wire []byte, reported bool) (implementation.Proposal, error) {
	deny := errors.New("engineering byte-hunk proposal rejected")
	if len(wire) == 0 || len(wire) > hermesruntime.MaxAttemptOutputBytes || reference.RejectDuplicateObjectKeys(wire) != nil {
		return implementation.Proposal{}, deny
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(wire, &top) != nil {
		return implementation.Proposal{}, deny
	}
	var edits []map[string]json.RawMessage
	if json.Unmarshal(top["edits"], &edits) != nil {
		return implementation.Proposal{}, deny
	}
	replacements := map[int]implementation.Edit{}
	for i, edit := range edits {
		if _, ok := edit["hunks"]; !ok {
			continue
		}
		for key := range edit {
			if key != "path" && key != "content" && key != "preimage_digest" && key != "preimage_mode" && key != "hunks" {
				return implementation.Proposal{}, deny
			}
		}
		if _, ok := edit["content"]; ok {
			return implementation.Proposal{}, deny
		}
		var e implementation.Edit
		body, _ := json.Marshal(edit)
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&e) != nil || !loop.EngineeringTextAsset(e.Path) || len(e.Hunks) == 0 || len(e.Hunks) > 64 || e.PreimageMode == 0 || e.PreimageMode > 0777 || !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(e.PreimageDigest) {
			return implementation.Proposal{}, deny
		}
		var hs []map[string]json.RawMessage
		if json.Unmarshal(edit["hunks"], &hs) != nil {
			return implementation.Proposal{}, deny
		}
		for _, h := range hs {
			if len(h) != 3 {
				return implementation.Proposal{}, deny
			}
			for _, key := range []string{"start", "end", "content"} {
				if raw, ok := h[key]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					return implementation.Proposal{}, deny
				}
			}
			c := bytes.TrimSpace(h["content"])
			if len(c) == 0 || c[0] != '"' {
				return implementation.Proposal{}, deny
			}
		}
		replacements[i] = e
		edits[i] = map[string]json.RawMessage{"path": edit["path"], "content": json.RawMessage(`""`)}
	}
	top["edits"], _ = json.Marshal(edits)
	normalized, _ := json.Marshal(top)
	proposal, err := decodePatch(normalized, reported)
	if err != nil {
		return proposal, err
	}
	for i, e := range replacements {
		proposal.Edits[i] = e
	}
	return proposal, nil
}
