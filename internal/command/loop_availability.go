package command

import (
	"errors"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/spf13/cobra"
)

func readLoopAvailabilityInput(id, path, action string) (app.SetLoopAvailabilityInput, error) {
	var input app.SetLoopAvailabilityInput
	if err := decodeJSONFile(path, &input); err != nil {
		return input, err
	}
	if input.Head.ID != id {
		return input, errors.New("loop path and head must match")
	}
	input.State = loop.AvailabilityArchived
	if action == "restore" {
		input.State = loop.AvailabilityPublished
	}
	return input, nil
}

func loopAvailabilityCommands(build builder) []*cobra.Command {
	inventory := &cobra.Command{Use: "inventory", Short: "List all Loop revisions including archived Loops", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		values, err := service.ListLoopInventory(cmd.Context())
		if err != nil {
			return err
		}
		return output(cmd, values)
	}}
	availability := &cobra.Command{Use: "availability LOOP", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.GetLoopAvailability(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	mutation := func(action string) *cobra.Command {
		return &cobra.Command{Use: action + " LOOP FILE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			input, err := readLoopAvailabilityInput(args[0], args[1], action)
			if err != nil {
				return usage(err)
			}
			service, err := build(cmd)
			if err != nil {
				return err
			}
			value, err := service.SetLoopAvailability(cmd.Context(), args[0], input)
			if err != nil {
				return err
			}
			return output(cmd, value)
		}}
	}
	return []*cobra.Command{inventory, availability, mutation("archive"), mutation("restore")}
}
