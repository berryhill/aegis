//go:build !linux

package migration

import (
	"context"
	"errors"
	"os/user"
)

const Confirmation = "migrate aegis to ~/.argis"

type Artifact struct{ Source, Destination, Kind string }
type Plan struct {
	SourceConfig      string     `json:"source_config"`
	SourceState       string     `json:"source_state"`
	SourceCheckpoints string     `json:"source_checkpoints"`
	DestinationRoot   string     `json:"destination_root"`
	DestinationConfig string     `json:"destination_config"`
	DestinationState  string     `json:"destination_state"`
	Artifacts         []Artifact `json:"artifacts"`
	Preserved         []string   `json:"preserve"`
	Confirmation      string     `json:"confirmation"`
	PrincipalUID      string     `json:"principal_uid"`
	PrincipalUser     string     `json:"principal_user"`
}
type Service struct {
	Home     func() (string, error)
	Current  func() (*user.User, error)
	LookupID func(string) (*user.User, error)
}

func New() *Service { return &Service{} }
func (*Service) Plan(context.Context) (Plan, error) {
	return Plan{}, errors.New("safe legacy migration requires Linux descriptor-anchored filesystem APIs; no mutation performed")
}
func (*Service) Apply(context.Context, Plan) error {
	return errors.New("safe legacy migration requires Linux descriptor-anchored filesystem APIs; no mutation performed")
}
func Digest(Plan) string { return "unsupported" }
