package cli

import (
	"fmt"

	"github.com/nikx-one/relayly/internal/config"
	"github.com/nikx-one/relayly/internal/database"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link <device-id-1> <device-id-2>",
	Short: "Pair two devices together for relaying",
	Long: `Establishes a symmetric pairing between two devices.
Messages from device 1 will be relayed to device 2, and vice versa.
Both devices must already be registered via 'relayly pair'.`,
	Args: cobra.ExactArgs(2),
	RunE: runLink,
}

func init() {
	rootCmd.AddCommand(linkCmd)
}

func runLink(cmd *cobra.Command, args []string) error {
	id1 := args[0]
	id2 := args[1]

	cfg, err := config.Load(cfgFile, cmd.Flags())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	db, err := database.Open(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Verify devices exist and are not already paired
	d1, err := db.GetDeviceByID(id1)
	if err != nil {
		return fmt.Errorf("device 1 (%s): %w", id1, err)
	}
	d2, err := db.GetDeviceByID(id2)
	if err != nil {
		return fmt.Errorf("device 2 (%s): %w", id2, err)
	}

	fmt.Printf("Linking %s (%s) <-> %s (%s)...\n", d1.Name, d1.ID, d2.Name, d2.ID)

	if err := db.PairDevices(id1, id2); err != nil {
		return fmt.Errorf("pairing failed: %w", err)
	}

	fmt.Println("✓  Devices linked successfully.")
	return nil
}
