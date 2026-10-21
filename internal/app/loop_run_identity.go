package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/berryhill/aegis/internal/graph"
	"sort"
)

// normalizeLoopRunIdentity is pure and shared by observation and reservation.
func normalizeLoopRunIdentity(input QueueLoopInput) (QueueLoopInput, string, error) {
	input.Inputs = append([]graph.NormalizedInput(nil), input.Inputs...)
	for i := range input.Inputs {
		wire, err := graph.CanonicalInputJSON(input.Inputs[i].Value)
		if err != nil {
			return input, "", err
		}
		input.Inputs[i].Value = wire
	}
	sort.Slice(input.Inputs, func(i, j int) bool { return input.Inputs[i].PortID < input.Inputs[j].PortID })
	identityInput := input
	identityInput.QueueItemID = ""
	wire, err := json.Marshal(identityInput)
	if err != nil {
		return input, "", err
	}
	digest := sha256.Sum256(wire)
	return input, hex.EncodeToString(digest[:]), nil
}
