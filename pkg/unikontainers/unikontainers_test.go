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

package unikontainers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/urunc-dev/urunc/pkg/unikontainers/hypervisors"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
	"github.com/urunc-dev/urunc/pkg/unikontainers/unikernels"
)

func TestConfineBlockSources(t *testing.T) {
	tests := []struct {
		name     string
		blocks   []types.BlockDevParams
		expected []string
	}{
		{
			name:     "explicit block image as the guest rootfs is confined",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "/rootfs.ext2", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/rootfs.ext2"},
		},
		{
			name:     "container rootfs as a block device keeps the device path",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "/dev/mapper/snap", IsExplicit: false}},
			expected: []string{"/dev/mapper/snap"},
		},
		{
			name: "block volumes from the container mounts keep the device path",
			blocks: []types.BlockDevParams{
				{ID: "rootfs", Source: "/dev/mapper/snap", IsExplicit: false},
				{ID: "vol1", Source: "/dev/loop3", IsExplicit: false},
			},
			expected: []string{"/dev/mapper/snap", "/dev/loop3"},
		},
		{
			name:     "block image from the annotation without a guest rootfs is confined",
			blocks:   []types.BlockDevParams{{ID: "annot_vol", Source: "/data/vol.ext2", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/data/vol.ext2"},
		},
		{
			name:     "parent references are clamped under the container rootfs",
			blocks:   []types.BlockDevParams{{ID: "rootfs", Source: "../../etc/shadow", IsExplicit: true}},
			expected: []string{containerRootfsMountPath + "/etc/shadow"},
		},
		{
			name:     "no block devices",
			blocks:   nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := confineBlockSources(tt.blocks)
			require.NoError(t, err)

			var sources []string
			for _, b := range got {
				sources = append(sources, b.Source)
			}
			assert.Equal(t, tt.expected, sources)
		})
	}
}

// The paths come from the image's annotations, so every one of them has to end
// up under the container rootfs mount, no matter how it is written.
func TestConfineToContainerRootfs(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
		wantErr  bool
	}{
		{name: "empty path stays empty", path: "", expected: ""},
		{name: "absolute path", path: "/unikernel/kernel", expected: containerRootfsMountPath + "/unikernel/kernel"},
		{name: "relative path", path: "unikernel/kernel", expected: containerRootfsMountPath + "/unikernel/kernel"},
		{name: "parent references are clamped", path: "/../../etc/passwd", expected: containerRootfsMountPath + "/etc/passwd"},
		{name: "root of the image", path: "/", expected: containerRootfsMountPath},
		{name: "comma is rejected", path: "/img,readonly=on", wantErr: true},
		{name: "equals is rejected", path: "/img=evil", wantErr: true},
		{name: "space is rejected", path: "/foo bar", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := confineToContainerRootfs(tt.path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// Every monitor gets the memory limit of the container. Without one, the
// others fall back to the configured default, while hyperlight-unikraft
// leaves the size to hluk, which knows what the runtime of the rootfs needs.
func TestGuestMemory(t *testing.T) {
	t.Parallel()

	const limitB = 1536 * 1024 * 1024
	limit := int64(limitB)
	limited := &specs.LinuxResources{Memory: &specs.LinuxMemory{Limit: &limit}}
	unlimited := &specs.LinuxResources{Memory: &specs.LinuxMemory{}}
	hyperlight := string(hypervisors.HyperlightVmm)
	qemu := string(hypervisors.QemuVmm)

	tests := []struct {
		name      string
		vmm       string
		resources *specs.LinuxResources
		expected  uint64
	}{
		{name: "qemu with a limit", vmm: qemu, resources: limited, expected: limitB},
		{name: "qemu without a limit", vmm: qemu, resources: unlimited, expected: 256 * 1024 * 1024},
		{name: "qemu without resources", vmm: qemu, resources: nil, expected: 256 * 1024 * 1024},
		{name: "hyperlight-unikraft with a limit", vmm: hyperlight, resources: limited, expected: limitB},
		{name: "hyperlight-unikraft without a limit", vmm: hyperlight, resources: unlimited, expected: 0},
		{name: "hyperlight-unikraft without resources", vmm: hyperlight, resources: nil, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defaultMem := guestDefaultMemoryMB(tt.vmm, 256)
			assert.Equal(t, tt.expected, monitorMemoryBytes(defaultMem, tt.resources))
		})
	}
}

// A command that the guest of hyperlight-unikraft would split differently is
// refused when the container is created; other monitors are not affected.
func TestNewRefusesHyperlightCommandThatWouldBeSplit(t *testing.T) {
	t.Parallel()

	shellArgs := []string{"sh", "-c", "echo hi && sleep 1"}

	t.Run("hyperlight-unikraft", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotHypervisor] = string(hypervisors.HyperlightVmm)
		annots[annotInitrd] = "/unikernel/initrd.cpio"

		_, err := New(writeBundleWithArgs(t, annots, shellArgs), "test-container", t.TempDir(), defaultUruncConfig())
		assert.ErrorIs(t, err, unikernels.ErrHyperlightCommand)

		_, err = New(writeBundleWithArgs(t, annots, []string{"/app/server", "--port=8080"}), "test-container", t.TempDir(), defaultUruncConfig())
		assert.NoError(t, err)
	})

	t.Run("a native kernel on hyperlight-unikraft runs no command", func(t *testing.T) {
		t.Parallel()
		annots := validAnnots()
		annots[annotHypervisor] = string(hypervisors.HyperlightVmm)

		_, err := New(writeBundleWithArgs(t, annots, shellArgs), "test-container", t.TempDir(), defaultUruncConfig())
		assert.NoError(t, err)
	})

	t.Run("qemu", func(t *testing.T) {
		t.Parallel()
		_, err := New(writeBundleWithArgs(t, validAnnots(), shellArgs), "test-container", t.TempDir(), defaultUruncConfig())
		assert.NoError(t, err)
	})
}

// writeBundleWithArgs writes a bundle like writeBundle, whose process runs argv.
func writeBundleWithArgs(t *testing.T, annots map[string]string, argv []string) string {
	t.Helper()

	bundle := t.TempDir()
	spec := specs.Spec{
		Version:     specs.Version,
		Process:     &specs.Process{Args: argv},
		Linux:       &specs.Linux{},
		Root:        &specs.Root{Path: rootfsDirName},
		Annotations: annots,
	}
	data, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(bundle, configFilename), data, 0600))

	return bundle
}

// hostNetVMM is a monitor that opens the guest's sockets itself.
type hostNetVMM struct{ types.VMM }

func (hostNetVMM) UsesHostNet() bool { return true }

func TestSetupGuestNetLeavesHostNetMonitorsAlone(t *testing.T) {
	t.Parallel()
	// An unknown network type would fail SetupNet, so success shows that the
	// interface of the container was never looked at.
	netArgs, err := setupGuestNet(hostNetVMM{}, "no-such-network", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, types.NetDevParams{HostNet: true}, netArgs)
}

// The resolv.conf of the container is written only for a guest on host
// networking, readable by the monitor user alone, and recorded for the
// monitor to pass on.
func TestWriteGuestResolvConf(t *testing.T) {
	t.Parallel()

	const content = "nameserver 10.96.0.10\nsearch default.svc.cluster.local\n"

	t.Run("host networking with a resolv.conf", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "resolv.conf")
		args := types.ExecArgs{Net: types.NetDevParams{HostNet: true}, ResolvConf: content}

		require.NoError(t, writeGuestResolvConf(&args, path))
		assert.Equal(t, path, args.ResolvFile)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	for name, args := range map[string]types.ExecArgs{
		"no host networking": {ResolvConf: content},
		"no resolv.conf":     {Net: types.NetDevParams{HostNet: true}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "resolv.conf")

			require.NoError(t, writeGuestResolvConf(&args, path))
			assert.Empty(t, args.ResolvFile)
			assert.NoFileExists(t, path)
		})
	}

	t.Run("a failed write fails", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "missing", "resolv.conf")
		args := types.ExecArgs{Net: types.NetDevParams{HostNet: true}, ResolvConf: content}

		require.Error(t, writeGuestResolvConf(&args, path))
		assert.Empty(t, args.ResolvFile)
	})
}

// Only a monitor that installs the resolv.conf in the guest gets it read.
func TestResolvConfForMonitor(t *testing.T) {
	t.Parallel()

	const content = "nameserver 10.96.0.10\n"
	resolvConf := filepath.Join(t.TempDir(), "resolv.conf")
	require.NoError(t, os.WriteFile(resolvConf, []byte(content), 0o600))
	mounts := []specs.Mount{{Destination: "/etc/resolv.conf", Source: resolvConf}}

	assert.Equal(t, content, resolvConfForMonitor(string(hypervisors.HyperlightVmm), mounts))
	assert.Empty(t, resolvConfForMonitor(string(hypervisors.QemuVmm), mounts))
	assert.Empty(t, resolvConfForMonitor(string(hypervisors.FirecrackerVmm), mounts))
}
