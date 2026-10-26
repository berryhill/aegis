package orchestration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
)

// No expected output is delivered to the implementer. Bytes are actual admitted
// source data and provenance, not a filename/shared workspace or an assertion.
type managedDoerImplementationInput struct {
	Task             string                   `json:"task"`
	Feedback         string                   `json:"feedback"`
	SourceType       loop.ValueType           `json:"source_type"`
	SourceText       *string                  `json:"source_text,omitempty"`
	SourceBytes      []byte                   `json:"source_bytes,omitempty"`
	SourceProvenance *GraphArtifactProvenance `json:"source_provenance,omitempty"`
	WritableFiles    []string                 `json:"writable_files"`
	MaxAttempts      uint16                   `json:"max_attempts"`
}

func managedOutputAbsent(c loop.DoerContract) error {
	resolved, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return errors.New("managed workspace must resolve exactly")
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	// Every parent must already exist and resolve inside the exact workspace.
	parent := filepath.Dir(filepath.Join(c.Workspace, c.VerifyFile))
	resolved, err = filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return errors.New("managed output parent invalid")
	}
	_, err = root.Lstat(c.VerifyFile)
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("managed output must initially be absent")
	}
	return nil
}

func managedWorkspacesSeparate(a, b loop.DoerContract) error {
	for _, pair := range [][2]string{{a.Workspace, b.Workspace}, {b.Workspace, a.Workspace}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return errors.New("managed workspaces overlap")
		}
	}
	return nil
}

func (w *QueueWorker) managedNodeSource(ctx context.Context, request WorkRequest, item queue.Item, runtime RuntimeRequest, source loop.ManagedDoerSource, c loop.DoerContract) (managedDoerImplementationInput, func(context.Context) error, error) {
	input := managedDoerImplementationInput{Task: c.Task, WritableFiles: append([]string(nil), c.WritableFiles...), MaxAttempts: c.MaxAttempts}
	if source.Text != nil {
		if len(runtime.ArtifactInputs) != 0 {
			return input, nil, ErrWorkerDenied
		}
		text := *source.Text
		input.SourceType, input.SourceText = loop.TypeString, &text
		return input, nil, nil
	}
	if source.Artifact == nil || len(runtime.ArtifactInputs) != 1 {
		return input, nil, ErrWorkerDenied
	}
	v := runtime.ArtifactInputs[0]
	p := v.Provenance()
	ref := source.Artifact
	if ref.ArtifactID != p.ArtifactID || ref.AttemptID != p.ProducerAttempt.ID || ref.RunID != p.ProducerExecution.ID || ref.ContentRef != p.ContentRef {
		return input, nil, ErrWorkerDenied
	}
	input.SourceType, input.SourceBytes, input.SourceProvenance = loop.TypeArtifact, v.Content(), &p
	check := func(ctx context.Context) error {
		actual, err := w.ReadGraphArtifactInput(ctx, GraphArtifactRequest{Subject: request.Subject, Authority: request.Authority, QueueItemID: item.ItemID, GraphRunID: item.GraphRunID, ProducerNodeID: p.ProducerNodeID, ConsumerNodeID: p.ConsumerNodeID, EdgeID: p.EdgeID, ProducerExecutionID: p.ProducerExecution.ID, ProducerAttemptID: p.ProducerAttempt.ID, ArtifactID: p.ArtifactID, DispositionID: p.Disposition.ID})
		if err != nil {
			return err
		}
		if actual.Provenance() != p || !bytes.Equal(actual.Content(), input.SourceBytes) {
			return ErrWorkerDenied
		}
		return nil
	}
	if err := check(ctx); err != nil {
		return input, nil, err
	}
	return input, check, nil
}
