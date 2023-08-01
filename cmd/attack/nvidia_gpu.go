// Copyright 2023 Chaos Mesh Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package attack

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/chaos-mesh/chaosd/cmd/server"
	"github.com/chaos-mesh/chaosd/pkg/core"
	"github.com/chaos-mesh/chaosd/pkg/server/chaosd"
	"github.com/chaos-mesh/chaosd/pkg/utils"
)

func NewNvGPUAttackCommand(uid *string) *cobra.Command {
	options := core.NewNvGPUCommand()
	dep := fx.Options(
		server.Module,
		fx.Provide(func() *core.NvGPUCommand {
			options.UID = *uid
			return options
		}),
	)

	cmd := &cobra.Command{
		Use:   "nv-gpu-stress <subcommand>",
		Short: "Nvidia GPU Stress attack related commands",
	}

	cmd.AddCommand(
		NewGPUPercentageCommand(dep, options),
		NewGPUMemCommand(dep, options),
	)

	return cmd
}

func NewGPUPercentageCommand(dep fx.Option, options *core.NvGPUCommand) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "perc [options]",
		Short: "continuously stress GPU out",
		Run: func(*cobra.Command, []string) {
			options.Action = core.NvGPUPercentageAction
			options.CompleteDefaults()
			utils.FxNewAppWithoutLog(dep, fx.Invoke(NvGPUAttackF)).Run()
		},
	}

	cmd.Flags().IntVarP(&options.Time, "time", "t", 300, "Load specifies P percent loading per GPU on worker. 0 is effectively a sleep (no load) and 100 is full loading.")
	cmd.Flags().IntVarP(&options.GPUID, "gpuid", "g", 0, "Burn which GPU")
	cmd.Flags().StringSliceVarP(&options.Options, "options", "o", []string{}, "extend gpu-burn options.")

	return cmd
}

func NewGPUMemCommand(dep fx.Option, options *core.NvGPUCommand) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mem [options]",
		Short: "continuously stress gpu memory out",
		Run: func(*cobra.Command, []string) {
			options.Action = core.NvGPUMemAction
			options.CompleteDefaults()
			utils.FxNewAppWithoutLog(dep, fx.Invoke(NvGPUAttackF)).Run()
		},
	}

	cmd.Flags().StringVarP(&options.Size, "size", "s", "10", "Size specifies N bytes consumed per GPU on worker, default is the total available memory. One can specify the size as % of total available memory or in units of B, KB/KiB, MB/MiB, GB/GiB, TB/TiB..")
	cmd.Flags().IntVarP(&options.GPUID, "gpuid", "g", 0, "Burn which GPU")
	cmd.Flags().StringSliceVarP(&options.Options, "options", "o", []string{}, "extend gpu-burn options.")

	return cmd
}

func NvGPUAttackF(chaos *chaosd.Server, options *core.StressCommand) {
	if err := options.Validate(); err != nil {
		utils.ExitWithError(utils.ExitBadArgs, err)
	}

	uid, err := chaos.ExecuteAttack(chaosd.NVGPUAttack, options, core.CommandMode)
	if err != nil {
		utils.ExitWithError(utils.ExitError, err)
	}

	utils.NormalExit(fmt.Sprintf("Attack Nvidia GPU %s successfully, uid: %s", options.Action, uid))
}
