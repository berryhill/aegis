//go:build !linux

package command

import "github.com/spf13/cobra"

func addGatewayRecovery(_ *cobra.Command, _ Dependencies, _ *rootOptions) {}
