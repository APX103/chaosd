// Copyright 2020 Chaos Mesh Authors.
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

package chaosd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"

	"github.com/go-logr/zapr"

	"github.com/chaos-mesh/chaos-mesh/pkg/bpm"
	"github.com/pingcap/log"
	"github.com/shirou/gopsutil/process"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/chaos-mesh/chaosd/pkg/core"
)

type nvGPUAttack struct{}

var NVGPUAttack AttackType = nvGPUAttack{}

const (
	NVGPUSTRESSORTOOL = "gpu_burn"
)

type Stressor struct {
	Workers int `json:"workers"`
}

type Stressors struct {
	GPUMemoryStressor     *GPUMemoryStressor     `json:"memory,omitempty"`
	GPUPercentageStressor *GPUPercentageStressor `json:"cpu,omitempty"`
}

func (in *Stressors) Normalize() (string, string, error) {
	cpuStressors := ""
	memoryStressors := ""
	if in.GPUMemoryStressor != nil && in.GPUMemoryStressor.Workers != 0 {
		memoryStressors += fmt.Sprintf(" --workers %d", in.GPUMemoryStressor.Workers)
		if len(in.GPUMemoryStressor.Size) != 0 {
			memoryStressors += fmt.Sprintf(" --size %s", in.GPUMemoryStressor.Size)
		}

		if in.GPUMemoryStressor.Options != nil {
			for _, v := range in.GPUMemoryStressor.Options {
				memoryStressors += fmt.Sprintf(" %v ", v)
			}
		}
	}
	if in.GPUPercentageStressor != nil && in.GPUPercentageStressor.Workers != 0 {
		cpuStressors += " --cpu-load-slice 10 --cpu-method sqrt"
		cpuStressors += fmt.Sprintf(" --cpu %d", in.GPUPercentageStressor.Workers)
		if in.GPUPercentageStressor.Load != nil {
			cpuStressors += fmt.Sprintf(" --cpu-load %d",
				*in.GPUPercentageStressor.Load)
		}

		if in.GPUPercentageStressor.Options != nil {
			for _, v := range in.GPUPercentageStressor.Options {
				cpuStressors += fmt.Sprintf(" %v ", v)
			}
		}
	}
	return cpuStressors, memoryStressors, nil
}

func (in *Stressors) Validate(root interface{}, path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}

	if in.GPUMemoryStressor == nil && in.GPUPercentageStressor == nil {
		return field.ErrorList{
			field.Invalid(path, in, "missing stressors"),
		}
	}
	return nil
}

type GPUPercentageStressor struct {
	Stressor `json:",inline"`
	Load     *int     `json:"load,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type GPUMemoryStressor struct {
	Stressor    `json:",inline"`
	Size        string   `json:"size,omitempty" webhook:"Bytes"`
	OOMScoreAdj int      `json:"oomScoreAdj,omitempty"`
	Options     []string `json:"options,omitempty"`
}

func (nvGPUAttack) Attack(options core.AttackConfig, _ Environment) (err error) {
	attack := options.(*core.StressCommand)
	stressors := Stressors{}
	var stressorTool string

	if attack.Action == core.NvGPUPercentageAction {
		stressorTool = NVGPUSTRESSORTOOL
		stressors.GPUPercentageStressor = &GPUPercentageStressor{
			Stressor: Stressor{
				Workers: attack.Workers,
			},
			Load:    &attack.Load,
			Options: attack.Options,
		}
	} else if attack.Action == core.NvGPUMemAction {
		stressorTool = NVGPUSTRESSORTOOL
		stressors.GPUMemoryStressor = &GPUMemoryStressor{
			Stressor: Stressor{
				Workers: attack.Workers,
			},
			Size:    attack.Size,
			Options: attack.Options,
		}
	}

	var stressorsStr string
	if attack.Action == core.StressCPUAction {
		stressorsStr, _, err = stressors.Normalize()
		if err != nil {
			return
		}
	} else if attack.Action == core.StressMemAction {
		_, stressorsStr, err = stressors.Normalize()
		if err != nil {
			return
		}
	}

	errs := stressors.Validate(nil, field.NewPath("stressors"))
	if len(errs) > 0 {
		return errors.New(errs.ToAggregate().Error())
	}

	log.Info("stressors normalize", zap.String("arguments", stressorsStr))

	cmd := bpm.DefaultProcessBuilder(stressorTool, strings.Fields(stressorsStr)...).
		Build(context.Background())

	// Build will set SysProcAttr.Pdeathsig = syscall.SIGTERM, and so stress-ng will exit while chaosd exit
	// so reset it here
	cmd.Cmd.SysProcAttr = &syscall.SysProcAttr{}

	zapLogger, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	logger := zapr.NewLogger(zapLogger)
	backgroundProcessManager := bpm.StartBackgroundProcessManager(nil, logger)
	_, err = backgroundProcessManager.StartProcess(context.Background(), cmd)
	if err != nil {
		return
	}

	attack.StressngPid = int32(cmd.Process.Pid)
	log.Info(fmt.Sprintf("Start %s process successfully", stressorTool), zap.String("command", cmd.String()), zap.Int32("Pid", attack.StressngPid))

	return nil
}

func (nvGPUAttack) Recover(exp core.Experiment, _ Environment) error {
	config, err := exp.GetRequestCommand()
	if err != nil {
		return err
	}
	attack := config.(*core.NvGPUCommand)
	proc, err := process.NewProcess(attack.GPUBurnPid)
	if err != nil {
		log.Warn("Failed to get process", zap.Error(err))
		if errors.Is(err, process.ErrorProcessNotRunning) || errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return err
	}

	procName, err := proc.Name()
	if err != nil {
		return err
	}

	if !strings.Contains(procName, NVGPUSTRESSORTOOL) {
		log.Warn("the process is not gpu-burn, maybe it is killed by manual")
		return nil
	}

	if err := proc.Kill(); err != nil {
		log.Error("the process kill failed", zap.Error(err))
		return err
	}

	return nil
}
