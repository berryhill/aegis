//go:build !linux

package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/orchestration"
)

func (s *Service) contributionExecutionObserver(context.Context, core.Subject, contribution.Contract, orchestration.WorkResult, execution.LaunchContract) (*orchestration.ContributionExecutionObserver, error) {
	return nil, contribution.ErrNativeQuiescenceUnavailable
}
