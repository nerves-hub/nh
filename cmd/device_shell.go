/*
Copyright © 2026 NervesHub

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package cmd

import (
	"github.com/spf13/cobra"
)

var deviceShellCmd = &cobra.Command{
	Use:   "shell <identifier>",
	Short: "Connect to a device's shell",
	Long: `Open an interactive local shell on a device over a websocket.

Input is sent to the device and its output is shown locally. The session stays
open until the shell ends or you press Ctrl-C. Requires an interactive
terminal.`,
	Args: deviceIdentifierArgs,
	RunE: runDeviceShell,
}

func init() {
	deviceCmd.AddCommand(deviceShellCmd)
}

func runDeviceShell(cmd *cobra.Command, args []string) error {
	return runDeviceTerminalSession(cmd, shellTopic(args[0]), args[0], shellProtocol)
}

// shellTopic is the Phoenix topic for a device's local shell.
func shellTopic(identifier string) string {
	return "user:local_shell:identifier-" + identifier
}
