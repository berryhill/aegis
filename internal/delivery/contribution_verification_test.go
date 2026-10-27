package delivery

import "testing"

func TestContributionIntentComparisonUsesCanonicalProducer(t *testing.T) {
	plan := objectPlan(t)
	plan.PR = 7
	plan.Completion = digest("fixture pre-closure reference, not authority")
	plan.Intake.Source.IssueID = 11
	plan.Intake.Source.URL.Number = 11
	for _, operation := range []Operation{Commit, Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue, TransferGraph} {
		t.Run(string(operation), func(t *testing.T) {
			intent, err := intentFor(plan, operation)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateIntentForPlan(plan, intent); err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []string{"id", "payload", "run", "contract", "candidate", "actor", "repository", "base", "branch", "head", "policy", "role", "epoch", "operation"} {
				observed := intent
				switch mutation {
				case "id":
					observed.ID = digest("foreign")
				case "payload":
					observed.Payload = digest("foreign")
				case "run":
					observed.Run = "foreign"
				case "contract":
					observed.Contract = "foreign"
				case "candidate":
					observed.Candidate = "foreign"
				case "actor":
					observed.Actor = "foreign"
				case "repository":
					observed.Repository = "foreign/repository"
				case "base":
					observed.Base = "foreign"
				case "branch":
					observed.Branch = "foreign"
				case "head":
					observed.Head = "cccccccccccccccccccccccccccccccccccccccc"
				case "policy":
					observed.Policy = "foreign"
				case "role":
					observed.RoleBinding = digest("foreign")
				case "epoch":
					observed.FenceEpoch++
				case "operation":
					observed.Operation = "unsupported.operation"
				}
				if ValidateIntentForPlan(plan, observed) == nil {
					t.Fatal("substituted intent accepted", mutation)
				}
			}
		})
	}
	for _, object := range plan.Objects {
		current := plan
		current.Object = object
		operation := objectOp(object)
		intent, err := intentFor(current, operation)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateIntentForPlan(current, intent); err != nil {
			t.Fatal(err)
		}
		current.Object.Raw = append([]byte(nil), object.Raw...)
		current.Object.Raw = append(current.Object.Raw, 'x')
		if ValidateIntentForPlan(current, intent) == nil {
			t.Fatal("substituted object accepted")
		}
	}
}
