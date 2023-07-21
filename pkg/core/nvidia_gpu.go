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

package core

import (
	"encoding/json"

	"github.com/pingcap/errors"
)

const (
	NvGPUPercentageAction = "perc"
	NvGPUMemAction        = "mem"
	//NvGPUMountAction      = "mount"
)

type NvGPUCommand struct {
	CommonAttackConfig

	Load       int      `json:"load,omitempty"`
	Workers    int      `json:"workers,omitempty"`
	GPUID      string   `json:"gpu-id,omitempty"`
	Size       string   `json:"size,omitempty"`
	Options    []string `json:"options,omitempty"`
	GPUBurnPid int32    `json:"gpu-burn-pid,omitempty"`
}

var _ AttackConfig = &NvGPUCommand{}

func (s *NvGPUCommand) Validate() error {
	if err := s.CommonAttackConfig.Validate(); err != nil {
		return err
	}
	if len(s.Action) == 0 {
		return errors.New("action not provided")
	}

	return nil
}

func (s *NvGPUCommand) CompleteDefaults() {
	if s.Workers == 0 {
		s.Workers = 1
	}
}

func (s NvGPUCommand) RecoverData() string {
	data, _ := json.Marshal(s)

	return string(data)
}

func NewNvGPUCommand() *NvGPUCommand {
	return &NvGPUCommand{
		CommonAttackConfig: CommonAttackConfig{
			Kind: NvGPUAttack,
		},
	}
}
