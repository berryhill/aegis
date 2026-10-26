package app

import (
	"context"
	"errors"
	"reflect"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// Read exact pinned nodes independently of aggregate terminal outcome. Historical
// repositories without node support keep the existing optional response shape.
func readGraphNodes(ctx context.Context, repository fleet.Repository, item queue.Item, nodes []graph.Node) ([]GraphNodeView, error) {
	r, ok := repository.(fleet.GraphNodeRepository)
	if !ok {
		return nil, nil
	}
	var result []GraphNodeView
	for _, node := range nodes {
		admission, err := r.GetGraphNode(ctx, item.GraphRunID, node.ID)
		if errors.Is(err, fleet.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if admission.Execution.GraphRunID != item.GraphRunID || admission.Execution.GraphNodeID != node.ID || admission.Execution.Loop != node.Loop || admission.Execution.Participant != node.Participant || admission.Claim.QueueItem.ID != item.ItemID || admission.Claim.QueueItem.Digest != item.Digest {
			return nil, fleet.ErrCorrupt
		}
		view := GraphNodeView{Admission: admission}
		checkpoint, err := r.GetGraphNodeCheckpoint(ctx, item.GraphRunID, node.ID)
		if err == nil {
			if !reflect.DeepEqual(checkpoint.Admission, admission) {
				return nil, fleet.ErrCorrupt
			}
			view.Disposition = &checkpoint.Completion.Disposition
			view.Artifact = checkpoint.Completion.Artifact
			view.Receipts = checkpoint.Completion.Receipts
		} else if !errors.Is(err, fleet.ErrNotFound) {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}
