package api

import (
	"sort"
	"strings"

	"github.com/berryhill/aegis/internal/app"
)

// queueParticipantLabel reports exact per-node facts, not the submission owner
// or a latest Registry lookup. NodeParticipants is populated alongside
// NodeRuntimes from the resolved immutable snapshot before claim. Executed
// records also retain exact references. Conflicting/malformed facts fail closed
// for that node rather than selecting an arbitrary first or latest participant.
func queueParticipantLabel(view app.QueueExecutionView) string {
	refs := make(map[string]string)
	invalid := make(map[string]bool)
	add := func(node, label string, valid bool) {
		previous, exists := refs[node]
		if node == "" || !valid || (exists && previous != label) {
			invalid[node] = true
		}
		refs[node] = label
	}
	for node, ref := range view.NodeParticipants {
		add(node, exactRevisionLabel(ref.ID, ref.Revision, ref.Digest), ref.Validate() == nil)
	}
	for _, child := range view.LoopExecutions {
		ref := child.Participant
		add(child.GraphNodeID, exactRevisionLabel(ref.ID, ref.Revision, ref.Digest), ref.Validate() == nil)
	}
	for node := range view.NodeRuntimes {
		if _, exists := refs[node]; !exists {
			invalid[node] = true
			refs[node] = ""
		}
	}
	nodes := make([]string, 0, len(refs))
	for node := range refs {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	labels := make([]string, 0, len(nodes))
	for _, node := range nodes {
		label := "Unavailable"
		if !invalid[node] {
			label = refs[node]
		}
		if node != "" {
			label = node + " · " + label
		}
		labels = append(labels, label)
	}
	if len(labels) == 0 {
		return "Unavailable"
	}
	return strings.Join(labels, "; ")
}
