package doerbinding

import (
	"errors"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
)

// BindManagedDoerNode binds one service-resolved exact node to its immutable v7
// contract and typed source. Inputs must come from the sealed Graph plan and,
// for an artifact, independently verified upstream checkpoint readback. This
// structural operation is not runtime admission, host consent or artifact proof.
func BindManagedDoerNode(revision loop.LoopRevision, node graph.Node, inputs []graph.NormalizedInput) (loop.DoerContract, loop.ManagedDoerSource, error) {
	denied := errors.New("exact managed Doer node binding required")
	if revision.SchemaVersion != loop.ManagedDoerSchemaVersion || node.Loop != loopReference(revision) || !sameLoopPorts(node.Inputs, revision.Inputs) || !sameLoopPorts(node.Outputs, revision.Outputs) {
		return loop.DoerContract{}, loop.ManagedDoerSource{}, denied
	}
	typed := make([]loop.DoerInput, len(inputs))
	for i, input := range inputs {
		typed[i] = loop.DoerInput{PortID: input.PortID, Type: loop.ValueType(input.Type), Value: input.Value}
	}
	source, err := loop.NormalizeManagedDoerBinding(revision, typed)
	if err != nil {
		return loop.DoerContract{}, loop.ManagedDoerSource{}, err
	}
	c := revision.ManagedDoer.Doer
	c.WritableFiles = append([]string(nil), c.WritableFiles...)
	text := *c.ExpectedText
	c.ExpectedText = &text
	return c, source, nil
}
