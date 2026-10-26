package app

import (
	"fmt"
	"github.com/berryhill/aegis/internal/delivery"
)

func retainedDeliveryPublication(proof delivery.ReadbackProof, p ContributionDeliveryPolicy, pr uint64) bool {
	i := proof.Intent
	actor, repository, effect := false, false, false
	for _, f := range proof.Facts {
		if f.HTTPStatus != 200 {
			continue
		}
		switch f.Kind {
		case "actor":
			actor = f.Login == p.Actor && f.ID != 0
		case "repository":
			repository = f.Target.Repository == i.Repository && f.ID == i.RepositoryID && f.FullName == i.Repository && f.Permissions != nil && f.Permissions.Push
		case "ref":
			if i.Operation == delivery.Publish && f.Target.Repository == i.Repository && f.Target.Ref == i.Branch && f.Object != nil && f.Object.SHA == i.Head {
				effect = true
			}
		case "pull":
			if (i.Operation == delivery.PullRequest || i.Operation == delivery.UpdatePullRequest) && f.HTMLURL == fmt.Sprintf("https://github.com/%s/pull/%d", i.Repository, pr) && f.Target.Repository == i.Repository && f.Number == pr && f.User != nil && f.User.Login == p.Actor && !f.Draft && f.Head != nil && f.Base != nil && f.Head.SHA == i.Head && f.Head.Ref == p.Branch && f.Base.Ref == p.Binding.Base && f.Base.SHA == p.BaseHead && f.Head.Repo.ID == i.RepositoryID && f.Base.Repo.ID == i.RepositoryID && f.Head.Repo.FullName == i.Repository && f.Base.Repo.FullName == i.Repository {
				effect = true
			}
		}
	}
	return actor && repository && effect
}
