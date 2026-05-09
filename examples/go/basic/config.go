package main

import (
	"flag"
	"os"
)

// Config holds the application configuration.
type Config struct {
	ServerURL string `env:"RELAYLY_SERVER" flag:"server"`
	DeviceID  string `env:"RELAYLY_DEVICE_ID" flag:"device-id"`
	KeyPath   string `env:"RELAYLY_KEY_PATH" flag:"key-path"`
}

// LoadConfig loads configuration from CLI flags, environment variables, and defaults.
func LoadConfig() *Config {
	cfg := &Config{}

	// Define flags
	serverURL := flag.String("server", "wss://relay.example.com", "Relay server URL")
	deviceID := flag.String("device-id", "my-laptop", "Unique ID for this device")
	keyPath := flag.String("key-path", "~/.relayly/device.key", "Path to the device private key")

	flag.Parse()

	// 1. Start with defaults/flags
	cfg.ServerURL = *serverURL
	cfg.DeviceID = *deviceID
	cfg.KeyPath = *keyPath

	// 2. Override with Environment Variables if present
	if env := os.Getenv("RELAYLY_SERVER"); env != "" {
		cfg.ServerURL = env
	}
	if env := os.Getenv("RELAYLY_DEVICE_ID"); env != "" {
		cfg.DeviceID = env
	}
	if env := os.Getenv("RELAYLY_KEY_PATH"); env != "" {
		cfg.KeyPath = env
	}

	// Note: In a larger app, we would also load from a config file here.
	// For this basic example, flags and env vars are sufficient to demonstrate the strategy.

	return cfg
}
