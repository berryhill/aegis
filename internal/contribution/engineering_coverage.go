package contribution

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// EngineeringReadChunk is controller-produced full-preimage coverage, not an edit
// or an authorization. Offsets count bytes; every selected byte is covered once.
type EngineeringReadChunk struct {
	Path   string
	Digest string
	Offset int
	Bytes  int
	Text   string
}
type EngineeringReadNote struct {
	Chunk        EngineeringReadChunk
	Observations string
}

const EngineeringChunkBytes = 8192
const MaxEngineeringReadChunks = 128

func EngineeringChunks(path, digest, text string) []EngineeringReadChunk {
	var chunks []EngineeringReadChunk
	for offset := 0; offset < len(text); {
		end := min(offset+EngineeringChunkBytes, len(text))
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		chunks = append(chunks, EngineeringReadChunk{path, digest, offset, len(text), text[offset:end]})
		offset = end
	}
	if len(text) == 0 {
		chunks = append(chunks, EngineeringReadChunk{Path: path, Digest: digest})
	}
	return chunks
}

// ValidateEngineeringCoverage reconstructs complete preimages from retained
// managed inputs. Notes are untrusted planning observations and never PASS proof.
func ValidateEngineeringCoverage(bodies, outputs []string) (map[string]string, []EngineeringReadNote, error) {
	texts := map[string]string{}
	notes := []EngineeringReadNote{}
	identities := map[string]EngineeringReadChunk{}
	deny := fmt.Errorf("invalid engineering read coverage")
	if len(bodies) != len(outputs) || len(bodies) > MaxEngineeringReadChunks {
		return nil, nil, deny
	}
	for i, body := range bodies {
		var input struct{ Chunk EngineeringReadChunk }
		var output struct {
			Observations string `json:"observations"`
		}
		if json.Unmarshal([]byte(body), &input) != nil || json.Unmarshal([]byte(outputs[i]), &output) != nil || len(output.Observations) > 2048 {
			return nil, nil, deny
		}
		c := input.Chunk
		if c.Path == "" || c.Offset != len(texts[c.Path]) || len(c.Text) > EngineeringChunkBytes || c.Bytes < 0 || c.Offset+len(c.Text) > c.Bytes || !utf8.ValidString(c.Text) {
			return nil, nil, deny
		}
		if prev, ok := identities[c.Path]; ok && (prev.Digest != c.Digest || prev.Bytes != c.Bytes) {
			return nil, nil, deny
		}
		identities[c.Path] = c
		texts[c.Path] += c.Text
		c.Text = ""
		notes = append(notes, EngineeringReadNote{c, output.Observations})
	}
	for p, text := range texts {
		c := identities[p]
		if len(text) != c.Bytes || Digest([]byte(text)) != c.Digest {
			return nil, nil, deny
		}
	}
	return texts, notes, nil
}
