package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// deviceCreds is what registerOrLoadDevice persists: the credentials this example
// needs to reconnect as the same device on future runs.
type deviceCreds struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

// registerOrLoadDevice returns persistent device credentials, registering a new
// device via POST /api/v1/devices the first time this runs and reusing the saved
// credentials afterward — the same load-or-create pattern relayly.LoadOrGenerateKey
// uses for the device's E2E identity key, just for the server-issued device token.
func registerOrLoadDevice(serverURL, credsPath, name string) (deviceID, deviceToken string, err error) {
	credsPath = expandHome(credsPath)

	if data, readErr := os.ReadFile(credsPath); readErr == nil {
		var creds deviceCreds
		if json.Unmarshal(data, &creds) == nil && creds.DeviceID != "" && creds.DeviceToken != "" {
			return creds.DeviceID, creds.DeviceToken, nil
		}
	}

	apiURL, err := devicesEndpoint(serverURL)
	if err != nil {
		return "", "", err
	}

	body, _ := json.Marshal(map[string]string{"name": name})
	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("registering device: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("registering device: server returned %d", resp.StatusCode)
	}

	var creds deviceCreds
	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return "", "", fmt.Errorf("decoding device response: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(credsPath), 0700); err != nil {
		return "", "", fmt.Errorf("creating credentials directory: %w", err)
	}
	data, _ := json.MarshalIndent(creds, "", "  ")
	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return "", "", fmt.Errorf("saving device credentials: %w", err)
	}

	return creds.DeviceID, creds.DeviceToken, nil
}

// devicesEndpoint converts a ws(s):// relay URL (with or without a trailing /ws)
// into its http(s) POST /api/v1/devices equivalent.
func devicesEndpoint(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", fmt.Errorf("invalid server URL: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	u.Path = "/api/v1/devices"
	u.RawQuery = ""
	return u.String(), nil
}

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
