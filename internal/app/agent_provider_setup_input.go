package app

// NewAgentProviderSetupInput is shared file-free CLI/manager syntax. It carries
// no caller identity; service admission remains mandatory for every operation.
func NewAgentProviderSetupInput(args []string) (AgentProviderSetupInput, error) {
	if len(args) == 0 {
		return AgentProviderSetupInput{}, ErrConflict
	}
	in := AgentProviderSetupInput{Action: args[0]}
	switch {
	case len(args) == 1 && (in.Action == "status" || in.Action == "models"):
	case len(args) == 2 && in.Action == "candidates":
		in.ProfileHome = args[1]
	case (len(args) == 2 || len(args) == 3) && (in.Action == "review" || in.Action == "setup"):
		in.ProfileHome = args[1]
		if len(args) == 3 {
			in.CredentialID = args[2]
		}
	case len(args) == 2 && in.Action == "connect":
		in.ReviewID = args[1]
	case len(args) == 3 && in.Action == "promote":
		in.Provider, in.Model = args[1], args[2]
	case len(args) == 3 && in.Action == "approve":
		in.ReviewID, in.CharterDigest = args[1], args[2]
	default:
		return AgentProviderSetupInput{}, ErrConflict
	}
	if !in.valid() {
		return AgentProviderSetupInput{}, ErrConflict
	}
	return in, nil
}
