// Package cli defines the Relayly command-line interface using Cobra.
package cli

import (
	"fmt"
	"os"

	"github.com/NIKX-Tech/relayly/pkg/version"
	"github.com/spf13/cobra"
)

var cfgFile string

// rootCmd is the base command. All subcommands attach to this.
var rootCmd = &cobra.Command{
	Use:   "relayly",
	Short: "A lightweight, self-hosted WebSocket relay for local-first apps",
	Long: `Relayly — secure, end-to-end encrypted WebSocket relay server.

It enables your own devices (phone, laptop, desktop) to communicate
directly through a relay you control, with no third-party in the loop.

Commands:
  relayly start        Start the relay and admin servers
  relayly status       Show connected devices and server status
  relayly pair <name>  Register a new device and print its pairing QR code`,
	Version: version.Info(),
}

// Execute is the entry point called from main().
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"config file (default: ./config/relayly.yaml)")
}
