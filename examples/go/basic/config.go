package main

import (
	"flag"
	"os"
)

// Config holds the application configuration.
type Config struct {
	ServerURL string `env:"RELAYLY_SERVER" flag:"server"`
	Name      string `env:"RELAYLY_DEVICE_NAME" flag:"device-name"`
	KeyPath   string `env:"RELAYLY_KEY_PATH" flag:"key-path"`
	CredsPath string `env:"RELAYLY_CREDS_PATH" flag:"creds-path"`
}

// LoadConfig loads configuration from CLI flags, environment variables, and defaults.
func LoadConfig() *Config {
	cfg := &Config{}

	// Define flags
	serverURL := flag.String("server", "wss://relay.example.com/ws", "Relay server URL")
	deviceName := flag.String("device-name", "my-laptop", "Display name to register this device under")
	keyPath := flag.String("key-path", "~/.relayly/device.key", "Path to the device private key")
	credsPath := flag.String("creds-path", "~/.relayly/basic-device.json", "Path to the registered device_id/device_token")

	flag.Parse()

	// 1. Start with defaults/flags
	cfg.ServerURL = *serverURL
	cfg.Name = *deviceName
	cfg.KeyPath = *keyPath
	cfg.CredsPath = *credsPath

	// 2. Override with Environment Variables if present
	if env := os.Getenv("RELAYLY_SERVER"); env != "" {
		cfg.ServerURL = env
	}
	if env := os.Getenv("RELAYLY_DEVICE_NAME"); env != "" {
		cfg.Name = env
	}
	if env := os.Getenv("RELAYLY_KEY_PATH"); env != "" {
		cfg.KeyPath = env
	}
	if env := os.Getenv("RELAYLY_CREDS_PATH"); env != "" {
		cfg.CredsPath = env
	}

	// Note: In a larger app, we would also load from a config file here.
	// For this basic example, flags and env vars are sufficient to demonstrate the strategy.

	return cfg
}
