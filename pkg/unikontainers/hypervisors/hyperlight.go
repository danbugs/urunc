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
	"errors"
	"strconv"
	"strings"

	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"golang.org/x/sys/unix"
)

const (
	HyperlightVmm    VmmType = "hyperlight-unikraft"
	HyperlightBinary string  = "hluk"
)

var ErrHyperlightNothingToBoot = errors.New("hyperlight-unikraft requires an initrd, a kernel or a snapshot")

// hyperlightGuestExecOpt is the hluk option that carries the guest command.
const hyperlightGuestExecOpt = "--guest-exec="

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

// SupportsControlSocket reports that Hyperlight exposes no control socket.
func (h *Hyperlight) SupportsControlSocket() bool {
	return false
}

// Path returns the path to the hluk binary.
func (h *Hyperlight) Path() string {
	return h.binaryPath
}

// Ok checks if the hluk binary is available.
// Binary availability is already verified by getVMMPath via exec.LookPath.
func (h *Hyperlight) Ok() error {
	return nil
}

func (h *Hyperlight) Signal(pid int, signal unix.Signal) error {
	return unix.Kill(pid, signal)
}

// BuildExecCmd constructs the hluk command line: the kernel of the image,
// the initrd (the rootfs CPIO) sized by its scratch memory, plus whatever the
// unikernel asks for through MonitorCli, such as the guest command. hluk
// embeds a Unikraft kernel of its own, so the unikernel binary is passed as
// --kernel, which boots it in place of the embedded one, and an image
// without one is left to the embedded kernel. An image with a kernel and no
// initrd boots a native Unikraft kernel, which is the workload itself. An
// image that ships a saved snapshot is resumed instead of booted.
func (h *Hyperlight) BuildExecCmd(args types.ExecArgs, ukernel types.Unikernel) ([]string, error) {
	extraMonArgs := ukernel.MonitorCli()
	// A snapshot is a saved guest, kernel and rootfs included, so hluk takes
	// no kernel, initrd or scratch memory with it, only what to run in it.
	if args.SnapshotPath != "" {
		cmdArgs := []string{h.binaryPath, "snapshot", "run", args.SnapshotPath}
		cmdArgs = append(cmdArgs, extraMonArgs.OtherArgs...)
		return cmdArgs, nil
	}
	// With neither an initrd nor a kernel there is nothing to boot, so fail
	// here rather than after the monitor started.
	if args.InitrdPath == "" && args.UnikernelPath == "" {
		return nil, ErrHyperlightNothingToBoot
	}
	otherArgs := extraMonArgs.OtherArgs
	// A native kernel, booted without an initrd, runs its own main(): there
	// is no runtime driver in it to run a guest command, and hluk refuses
	// one. Leave the command out instead of failing the container.
	if args.InitrdPath == "" {
		otherArgs = withoutGuestExec(otherArgs)
	}

	cmdArgs := []string{h.binaryPath, "run"}
	if args.UnikernelPath != "" {
		cmdArgs = append(cmdArgs, "--kernel", args.UnikernelPath)
	}
	if args.InitrdPath != "" {
		cmdArgs = append(cmdArgs, "--initrd", args.InitrdPath)
	}
	// hluk sizes the guest by its scratch memory in MiB. A limit too small
	// to express in MiB is left to hluk's own default.
	if memMiB := bytesToMiB(args.MemSizeB); memMiB > 0 {
		cmdArgs = append(cmdArgs, "--scratch-mb", strconv.FormatUint(memMiB, 10))
	}
	cmdArgs = append(cmdArgs, otherArgs...)

	return cmdArgs, nil
}

// withoutGuestExec returns args without the guest command.
func withoutGuestExec(args []string) []string {
	kept := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, hyperlightGuestExecOpt) {
			vmmLog.WithField("option", arg).Warn("a native kernel runs no guest command, leaving it out")
			continue
		}
		kept = append(kept, arg)
	}
	return kept
}

// PreExec performs pre-execution setup. Hyperlight has no special pre-exec requirements.
func (h *Hyperlight) PreExec(_ types.ExecArgs) error {
	return nil
}
