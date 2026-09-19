//go:build !linux

package reset

import (
	"context"
	"errors"
)

func requireUnboundSocket(string) error {
	return errors.New("safe orphan transport recovery requires Linux")
}

func removeOrphan(context.Context, Artifact, []Artifact) error {
	return errors.New("safe orphan transport recovery requires Linux descriptor-anchored deletion")
}
