package orchestration

import (
	"github.com/berryhill/aegis/internal/doerbinding"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
)

// BindDirectDoerContract is the application-facing orchestration boundary for
// an exact reusable Loop's immutable direct-run inputs. It grants no authority;
// callers must resolve canonical definitions and independently admit execution.
func BindDirectDoerContract(revision loop.LoopRevision, node graph.Node, snapshot graph.GraphRunSnapshot) (loop.DoerContract, error) {
	return doerbinding.BindDoerDirectRun(revision, node, snapshot)
}
