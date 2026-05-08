package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var (
	statusAdminURL string
	statusFormat   string
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show relay server status and connected devices",
	RunE:  runStatus,
}

func init() {
	statusCmd.Flags().StringVar(&statusAdminURL, "admin-url", "http://127.0.0.1:8081",
		"Admin server URL")
	statusCmd.Flags().StringVar(&statusFormat, "format", "text",
		"Output format: text | json")
	rootCmd.AddCommand(statusCmd)
}

type statusResp struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Connected int    `json:"connected"`
	Uptime    string `json:"uptime"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	resp, err := http.Get(statusAdminURL + "/api/v1/status")
	if err != nil {
		return fmt.Errorf("connecting to admin server at %s: %w\n"+
			"  → Is `relayly start` running?", statusAdminURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var s statusResp
	if err := json.Unmarshal(body, &s); err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	if statusFormat == "json" {
		fmt.Println(string(body))
		return nil
	}

	// Human-readable output
	fmt.Printf("  Status:    %s\n", s.Status)
	fmt.Printf("  Version:   %s\n", s.Version)
	fmt.Printf("  Connected: %d device(s)\n", s.Connected)
	fmt.Printf("  Uptime:    %s\n", s.Uptime)
	return nil
}
