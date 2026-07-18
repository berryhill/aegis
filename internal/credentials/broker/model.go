package broker

import (
	"context"
	"errors"
	"time"
)

var ErrDownstream = errors.New("broker downstream action failed")

const (
	ActionGitHubGetRepository = "github.get_repository.v1"
	GitHubScope               = "github/read"
	GitHubDestination         = "github-api"
)

type Peer struct {
	PID int32
	UID uint32
	GID uint32
}

type Request struct {
	SchemaVersion uint16    `json:"schema_version"`
	RequestID     string    `json:"request_id"`
	Deadline      time.Time `json:"deadline"`
	Capability    string    `json:"capability"`
	Owner         string    `json:"owner"`
	Repository    string    `json:"repository"`
}

type Capability struct {
	SessionID     string
	MandateID     string
	SubjectID     string
	AgentID       string
	StanzaID      string
	DeploymentID  string
	CharterDigest string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	RuntimePID    int
	ProcessStart  string
}

type Grant struct {
	RequestID       string
	SessionID       string
	MandateID       string
	SubjectID       string
	AgentID         string
	StanzaID        string
	DeploymentID    string
	CharterDigest   string
	Scope           string
	Operation       string
	Destination     string
	RecordID        string
	Version         uint64
	BindingRevision uint64
}

type Result struct {
	StatusCode int        `json:"status_code"`
	Outcome    string     `json:"outcome"`
	RequestID  string     `json:"request_id"`
	Repository Repository `json:"repository"`
}

type Repository struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	Archived      bool   `json:"archived"`
	Visibility    string `json:"visibility"`
	UpdatedAt     string `json:"updated_at"`
}

type Executor func(context.Context, []byte, Grant) (Result, error)

type Authorizer interface {
	ExecuteBroker(context.Context, Peer, Request, Executor) (Result, error)
}
