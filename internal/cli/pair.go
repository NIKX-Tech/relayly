package cli

import (
	"fmt"
	"os"

	"github.com/NIKX-Tech/relayly/internal/config"
	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/pairing"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

var (
	pairQRSize int
	pairNoQR   bool
)

var pairCmd = &cobra.Command{
	Use:   "pair <device-name>",
	Short: "Register a new device and print its pairing QR code",
	Long: `Creates a new device registration in the database and outputs:
  • A pairing token
  • A QR code (scan with your Relayly client app to pair)

Example:
  relayly pair myphone
  relayly pair "Erfan's MacBook Pro"`,
	Args: cobra.ExactArgs(1),
	RunE: runPair,
}

func init() {
	pairCmd.Flags().IntVar(&pairQRSize, "qr-size", 256, "QR code pixel size")
	pairCmd.Flags().BoolVar(&pairNoQR, "no-qr", false, "Skip QR code output (print token only)")
	rootCmd.AddCommand(pairCmd)
}

func runPair(cmd *cobra.Command, args []string) error {
	deviceName := args[0]

	// Load config to find the database
	cfg, err := config.Load(cfgFile, cmd.Flags())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	db, err := database.Open(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Create the device
	device, err := pairing.NewDevice(deviceName)
	if err != nil {
		return fmt.Errorf("generating device: %w", err)
	}

	if err := db.CreateDevice(device); err != nil {
		return fmt.Errorf("saving device: %w", err)
	}

	fmt.Printf("\n✓  Device registered: %q\n", device.Name)
	fmt.Printf("   ID:    %s\n", device.ID)
	fmt.Printf("   Token: %s\n\n", device.PairToken)

	if !pairNoQR {
		// Encode as a URI for the client to parse:
		// relayly://pair?device_id=<id>&token=<token>
		pairingURI := fmt.Sprintf("relayly://pair?device_id=%s&token=%s",
			device.ID, device.PairToken)

		fmt.Println("Scan this QR code with your Relayly client app:")
		fmt.Println()

		// Print QR code to terminal as ASCII
		q, err := qrcode.New(pairingURI, qrcode.Medium)
		if err != nil {
			return fmt.Errorf("generating qr code: %w", err)
		}

		// ASCII art to stdout
		fmt.Println(q.ToSmallString(false))
		fmt.Printf("  URI: %s\n\n", pairingURI)

		// Also write a PNG file for sharing
		pngPath := fmt.Sprintf("pair-%s.png", device.ID[:8])
		if err := q.WriteFile(pairQRSize, pngPath); err == nil {
			fmt.Printf("  QR saved: %s\n", pngPath)
		}
	}

	fmt.Println("Run `relayly pair <peer-name>` on your second device,")
	fmt.Println("then use `relayly status` to see them both online.")
	fmt.Println()

	_ = os.Stdout.Sync()
	return nil
}
