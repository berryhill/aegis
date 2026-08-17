package command

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/spf13/cobra"
)

func decodeJSONFile(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func fleetAgentsCmd(build builder) *cobra.Command {
	command := &cobra.Command{Use: "agents", Aliases: []string{"agent"}, Short: "Register and inspect executable fleet participants"}
	register := &cobra.Command{Use: "register FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.RegisterFleetAgentInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, created, err := service.RegisterFleetAgent(cmd.Context(), input)
		if err != nil {
			return err
		}
		return output(cmd, map[string]any{"agent": value, "created": created})
	}}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		values, err := service.ListFleetAgents(cmd.Context())
		if err != nil {
			return err
		}
		return output(cmd, values)
	}}
	show := &cobra.Command{Use: "show AGENT [REVISION]", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.GetFleetAgent(cmd.Context(), args[0], revision)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	history := &cobra.Command{Use: "history AGENT", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		values, err := service.ListFleetAgentRevisions(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return output(cmd, values)
	}}
	lifecycleCommand := func(name string, state registry.Lifecycle) *cobra.Command {
		return &cobra.Command{Use: name + " AGENT FILE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
			var input app.SetAgentLifecycleInput
			if err := decodeJSONFile(args[1], &input); err != nil {
				return usage(err)
			}
			input.Lifecycle = state
			service, err := build(cmd)
			if err != nil {
				return err
			}
			value, err := service.SetAgentLifecycle(cmd.Context(), args[0], input)
			if err != nil {
				return err
			}
			return output(cmd, value)
		}}
	}
	enable := lifecycleCommand("enable", registry.LifecycleEnabled)
	disable := lifecycleCommand("disable", registry.LifecycleDisabled)
	retire := lifecycleCommand("retire", registry.LifecycleRetired)
	command.AddCommand(register, list, show, history, enable, disable, retire)
	return command
}

func fleetLoopsCmd(build builder) *cobra.Command {
	command := &cobra.Command{Use: "loops", Aliases: []string{"loop"}, Short: "Publish and inspect immutable Loop revisions"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		values, err := service.ListLoops(cmd.Context())
		if err != nil {
			return err
		}
		return output(cmd, values)
	}}
	publish := &cobra.Command{Use: "publish FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.PublishLoopInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.PublishLoop(cmd.Context(), input)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	show := &cobra.Command{Use: "show LOOP REVISION", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.GetLoop(cmd.Context(), args[0], revision)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	command.AddCommand(list, publish, show)
	return command
}

func fleetGraphsCmd(build builder) *cobra.Command {
	command := &cobra.Command{Use: "graphs", Aliases: []string{"graph"}, Short: "Publish, inspect, and submit immutable Graph revisions"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		values, err := service.ListGraphs(cmd.Context())
		if err != nil {
			return err
		}
		return output(cmd, values)
	}}
	publish := &cobra.Command{Use: "publish FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.PublishGraphInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.PublishGraph(cmd.Context(), input)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	show := &cobra.Command{Use: "show GRAPH REVISION", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.GetGraph(cmd.Context(), args[0], revision)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	submit := &cobra.Command{Use: "submit FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.SubmitGraphInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.SubmitGraph(cmd.Context(), input)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	command.AddCommand(list, publish, show, submit)
	return command
}

func fleetQueueCmd(build builder) *cobra.Command {
	command := &cobra.Command{Use: "queue", Short: "Inspect and process authority-bound Execution Queue items"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		values, err := service.ListQueue(cmd.Context())
		if err != nil {
			return err
		}
		return output(cmd, values)
	}}
	show := &cobra.Command{Use: "show ITEM", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.GetQueueItem(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	process := &cobra.Command{Use: "process FILE", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.ProcessQueueItemInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		service, err := build(cmd)
		if err != nil {
			return err
		}
		value, err := service.ProcessQueueItem(cmd.Context(), input)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	command.AddCommand(list, show, process)
	return command
}

func exactRevision(args []string) (uint64, error) {
	if len(args) < 2 {
		return 0, nil
	}
	revision, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil || revision == 0 {
		return 0, errors.New("revision must be a positive integer")
	}
	return revision, nil
}
