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
	NVGPUPSTRESSORTOOL = "gpu_burn"
	NVGPUMSTRESSORTOOL = "gpu_burn"
)

type Stressor struct {
	Workers int `json:"workers"`
}

type NVGPUStressors struct {
	GPUMemoryStressor     *GPUMemoryStressor     `json:"memory,omitempty"`
	GPUPercentageStressor *GPUPercentageStressor `json:"cpu,omitempty"`
}

func (in *NVGPUStressors) Normalize() (string, string, error) {
	log.Info("Nomalizing")
	gpuPercentageStressors := ""
	gpuMemoryStressors := ""
	if in.GPUMemoryStressor != nil && in.GPUMemoryStressor.Workers != 0 {
		if len(in.GPUMemoryStressor.Size) != 0 {
			gpuMemoryStressors += fmt.Sprintf(" -m %s%", in.GPUMemoryStressor.Size)
		}

		if in.GPUPercentageStressor.GPUID != -1 {
			gpuMemoryStressors += fmt.Sprintf(" -i %d", in.GPUMemoryStressor.GPUID)
		}

		if in.GPUMemoryStressor.Options != nil {
			for _, v := range in.GPUMemoryStressor.Options {
				gpuMemoryStressors += fmt.Sprintf(" %v ", v)
			}
		}
	}
	if in.GPUPercentageStressor != nil && in.GPUPercentageStressor.Workers != 0 {
		if in.GPUPercentageStressor.Time != -1 {
			gpuPercentageStressors += fmt.Sprintf(" %d", in.GPUPercentageStressor.Time)
		}

		if in.GPUPercentageStressor.GPUID != -1 {
			gpuPercentageStressors += fmt.Sprintf(" -i %d", in.GPUPercentageStressor.GPUID)
		}

		if in.GPUPercentageStressor.Options != nil {
			for _, v := range in.GPUPercentageStressor.Options {
				gpuPercentageStressors += fmt.Sprintf(" %v ", v)
			}
		}
	}
	log.Info(gpuPercentageStressors)
	log.Info(gpuMemoryStressors)
	return gpuPercentageStressors, gpuMemoryStressors, nil
}

func (in *NVGPUStressors) Validate(root interface{}, path *field.Path) field.ErrorList {
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
	Time     int      `json:"load,omitempty"`
	GPUID    int      `json:"gpu-id,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type GPUMemoryStressor struct {
	Stressor `json:",inline"`
	Size     string   `json:"size,omitempty" webhook:"Bytes"`
	GPUID    int      `json:"gpu-id,omitempty"`
	Options  []string `json:"options,omitempty"`
}

func (nvGPUAttack) Attack(options core.AttackConfig, _ Environment) (err error) {
	attack := options.(*core.NvGPUCommand)
	stressors := NVGPUStressors{}
	var stressorTool string

	if attack.Action == core.NvGPUPercentageAction {
		stressorTool = NVGPUMSTRESSORTOOL
		stressors.GPUPercentageStressor = &GPUPercentageStressor{
			Stressor: Stressor{
				Workers: attack.Workers,
			},
			Time:    attack.Time,
			Options: attack.Options,
		}
	} else if attack.Action == core.NvGPUMemAction {
		stressorTool = NVGPUPSTRESSORTOOL
		stressors.GPUMemoryStressor = &GPUMemoryStressor{
			Stressor: Stressor{
				Workers: attack.Workers,
			},
			Size:    attack.Size,
			Options: attack.Options,
		}
	}

	var stressorsStr string
	log.Info(attack.Action)
	if attack.Action == core.NvGPUPercentageAction {
		stressorsStr, _, err = stressors.Normalize()
		if err != nil {
			return
		}
	} else if attack.Action == core.NvGPUMemAction {
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

	attack.GPUBurnPid = int32(cmd.Process.Pid)
	log.Info(fmt.Sprintf("Start %s process successfully", stressorTool), zap.String("command", cmd.String()), zap.Int32("Pid", attack.GPUBurnPid))

	return nil
}

func (nvGPUAttack) Recover(exp core.Experiment, _ Environment) error {
	config, err := exp.GetRequestCommand()
	if err != nil {
		return err
	}
	attack := config.(*core.NvGPUCommand)
	log.Info("gpu-burn PID", zap.Int32("pid", attack.GPUBurnPid))
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

	if !strings.Contains(procName, NVGPUPSTRESSORTOOL) && !strings.Contains(procName, NVGPUMSTRESSORTOOL) {
		log.Warn("the process is not gpu-burn, maybe it is killed by manual")
		return nil
	}

	if err := proc.Kill(); err != nil {
		log.Error("the process kill failed", zap.Error(err))
		return err
	}

	return nil
}
