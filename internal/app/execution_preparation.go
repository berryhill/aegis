package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
)

// PartitionExecutionViews retains never-bound preparation, including terminal
// records, outside executable history. Current lifecycle remains projection-based.
func PartitionExecutionViews(views []QueueExecutionView) (executable, preparation []QueueExecutionView) {
	executable = []QueueExecutionView{}
	preparation = []QueueExecutionView{}
	for _, view := range views {
		if view.Projection.State.IsPreparation() || ((view.Item.State.IsPreparation() || view.Submission.AuthorityKind == "registered-agent-workspace") && view.RuntimeAuthorityBinding == nil) {
			preparation = append(preparation, view)
		} else {
			executable = append(executable, view)
		}
	}
	return
}

func (s *Service) ListExecutableQueueAs(ctx context.Context, subject core.Subject) ([]QueueExecutionView, error) {
	views, err := s.ListQueueAs(ctx, subject)
	if err != nil {
		return nil, err
	}
	executable, _ := PartitionExecutionViews(views)
	return executable, nil
}

func (s *Service) ListPreparationsAs(ctx context.Context, subject core.Subject) ([]QueueExecutionView, error) {
	views, err := s.ListQueueAs(ctx, subject)
	if err != nil {
		return nil, err
	}
	_, preparation := PartitionExecutionViews(views)
	return preparation, nil
}

func (s *Service) ListPreparations(ctx context.Context) ([]QueueExecutionView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListPreparationsAs(ctx, subject)
}
