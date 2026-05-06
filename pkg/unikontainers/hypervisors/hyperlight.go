// Copyright (c) 2023-2026, Nubificus LTD
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hypervisors

import (
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

const (
	HyperlightVmm    VmmType = "hyperlight"
	HyperlightBinary string  = ""
)

type Hyperlight struct {
	binaryPath string
	binary     string
}

// Stop kills the hyperlight process
func (h *Hyperlight) Stop(pid int) error {
	return killProcess(pid)
}

// UsesKVM returns a bool value depending on if the monitor uses KVM
func (h *Hyperlight) UsesKVM() bool {
	return true
}

// SupportsSharedfs returns a bool value depending on the monitor support for shared-fs
func (h *Hyperlight) SupportsSharedfs(_ string) bool {
	return false
}

// Path returns the path to the hyperlight binary.
func (h *Hyperlight) Path() string {
	return h.binaryPath
}

// Ok checks if the hyperlight binary is available.
// Since hyperlight is embedded, we just return nil.
func (h *Hyperlight) Ok() error {
	return nil
}

func (h *Hyperlight) BuildExecCmd(args types.ExecArgs, _ types.Unikernel) ([]string, error) {
	// Hyperlight is an embedded VMM, so we just run the unikernel directly.
	cmdArgs := []string{args.UnikernelPath}
	if args.Command != "" {
		cmdArgs = append(cmdArgs, strings.Split(args.Command, " ")...)
	}
	return cmdArgs, nil
}

// PreExec performs pre-execution setup. Hyperlight has no special pre-exec requirements.
func (h *Hyperlight) PreExec(_ types.ExecArgs) error {
	return nil
}
