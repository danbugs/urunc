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

package unikernels

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/urunc-dev/urunc/pkg/unikontainers/types"
)

func TestUnikraftMonitorCli(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		unikraft *Unikraft
		expected types.MonitorCliArgs
	}{
		{
			name:     "qemu takes everything from the kernel command line",
			unikraft: &Unikraft{Monitor: "qemu", Command: "/app --flag"},
			expected: types.MonitorCliArgs{},
		},
		{
			name:     "firecracker takes everything from the kernel command line",
			unikraft: &Unikraft{Monitor: "firecracker", Command: "/app --flag"},
			expected: types.MonitorCliArgs{},
		},
		{
			name:     "hyperlight passes the command as --guest-exec",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft", Command: "/entrypoint.py --fast"},
			expected: types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=/entrypoint.py --fast"}},
		},
		{
			name:     "hyperlight keeps a leading dash inside the command value",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft", Command: "--net"},
			expected: types.MonitorCliArgs{OtherArgs: []string{"--guest-exec=--net"}},
		},
		{
			name:     "hyperlight without a command leaves the entrypoint to hluk",
			unikraft: &Unikraft{Monitor: "hyperlight-unikraft"},
			expected: types.MonitorCliArgs{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.unikraft.MonitorCli())
		})
	}
}

// hluk passes the command to the guest as one string that the guest splits
// again, so only a command that survives the round trip is accepted.
func TestValidateHyperlightCommand(t *testing.T) {
	t.Parallel()

	manyArgs := make([]string, hyperlightMaxArgs+1)
	for i := range manyArgs {
		manyArgs[i] = "a"
	}
	longArg := strings.Repeat("a", hyperlightMaxCmdBytes-len("/app/x "))

	testCases := []struct {
		name    string
		argv    []string
		wantErr bool
	}{
		{name: "no command leaves the entrypoint", argv: nil},
		{name: "plain arguments", argv: []string{"/app/server", "--port=8080", "-v"}},
		{name: "as many arguments as the guest takes", argv: manyArgs[:hyperlightMaxArgs]},
		{name: "as long a command as the guest takes", argv: []string{"/app/x", longArg}},
		{name: "a shell script in one argument", argv: []string{"sh", "-c", "echo hi && sleep 5"}, wantErr: true},
		{name: "a tab", argv: []string{"/app/x", "a\tb"}, wantErr: true},
		{name: "a newline", argv: []string{"/app/x", "a\nb"}, wantErr: true},
		{name: "a no-break space", argv: []string{"/app/x", "a b"}, wantErr: true},
		{name: "an empty argument", argv: []string{"/bin/echo", "a", "", "b"}, wantErr: true},
		{name: "a single quote", argv: []string{"/app/x", "it's"}, wantErr: true},
		{name: "a double quote", argv: []string{"/app/x", `"a"`}, wantErr: true},
		{name: "a backslash", argv: []string{"/app/x", `a\b`}, wantErr: true},
		{name: "too many arguments", argv: manyArgs, wantErr: true},
		{name: "too long a command", argv: []string{"/app/x", longArg + "a"}, wantErr: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateHyperlightCommand(tc.argv)
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrHyperlightCommand)
				return
			}
			assert.NoError(t, err)
		})
	}
}
