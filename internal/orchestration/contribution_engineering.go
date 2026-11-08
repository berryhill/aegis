package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
)

// ResolveContributionEngineeringContext is the application-to-native admission
// boundary. It observes only explicit inputs; it creates no worktree or authority.
func ResolveContributionEngineeringContext(ctx context.Context, template loop.VerifiedImplementation, task, acceptance, workspace, repository string, writable, contextFiles []string) (loop.VerifiedImplementation, error) {
	return implementation.ResolveEngineeringContext(ctx, template, task, acceptance, workspace, repository, writable, contextFiles)
}
