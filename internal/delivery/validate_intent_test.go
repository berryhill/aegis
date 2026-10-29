package delivery

import "testing"

// Synthetic policy/role fixture over a real local Git object graph.
func TestValidateProjectedIntentAllClosedStages(t *testing.T) {
	p := objectPlan(t)
	for _, op := range []Operation{Commit, Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue, TransferGraph, UploadBlob, UploadTree, UploadCommit} {
		t.Run(string(op), func(t *testing.T) {
			plans := []Plan{p}
			if op == UploadBlob || op == UploadTree || op == UploadCommit {
				plans = nil
				for _, o := range p.Objects {
					if objectOp(o) == op {
						child := p
						child.Object = o
						plans = append(plans, child)
					}
				}
				if len(plans) == 0 {
					t.Fatal("missing graph kind")
				}
			}
			for _, child := range plans {
				i, err := intentFor(child, op)
				if err != nil {
					t.Fatal(err)
				}
				if ValidateProjectedIntent(p, i) != nil {
					t.Fatal("exact intent denied")
				}
				changes := []func(*Intent){func(i *Intent) { i.ID += "x" }, func(i *Intent) { i.Payload += "x" }, func(i *Intent) { i.Policy += "x" }, func(i *Intent) { i.Actor += "x" }, func(i *Intent) { i.Repository += "x" }, func(i *Intent) { i.RepositoryID++ }, func(i *Intent) { i.Base += "x" }, func(i *Intent) { i.Branch += "x" }, func(i *Intent) { i.Head += "x" }, func(i *Intent) { i.BaseHead += "x" }, func(i *Intent) { i.Candidate += "x" }, func(i *Intent) { i.RoleBinding += "x" }, func(i *Intent) { i.FenceEpoch++ }, func(i *Intent) { i.PR++ }, func(i *Intent) { i.Operation = "unknown" }, func(i *Intent) { i.ExpectedOld += "x" }, func(i *Intent) { i.Run += "x" }, func(i *Intent) { i.Contract += "x" }}
				for n, change := range changes {
					bad := i
					change(&bad)
					if ValidateProjectedIntent(p, bad) == nil {
						t.Fatalf("tamper %d accepted", n)
					}
				}
				narrow := p
				narrow.Delivery = "local-only"
				if op != Commit && ValidateProjectedIntent(narrow, i) == nil {
					t.Fatal("ceiling escaped")
				}
			}
		})
	}
}
