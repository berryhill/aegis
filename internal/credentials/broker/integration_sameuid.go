//go:build integration_sameuid

package broker

import "context"

// ServeSameUIDForIntegrationTest exercises the production server implementation
// without its service/runtime UID separation gate. It is compiled only for the
// explicit local integration test tag; production builds cannot call it.
func ServeSameUIDForIntegrationTest(ctx context.Context, authorizer Authorizer, config ServerConfig) error {
	return serve(ctx, authorizer, config, true)
}
