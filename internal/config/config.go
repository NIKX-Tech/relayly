// Package config loads and validates Relayly's configuration using Viper.
// Configuration is read from (in priority order):
//  1. Environment variables prefixed with RELAYLY_
//  2. relayly.local.yaml (local overrides, gitignored)
//  3. relayly.yaml (defaults shipped with the binary)
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config is the top-level configuration object.
type Config struct {
	Host      string    `mapstructure:"host"`
	Port      int       `mapstructure:"port"`
	TLS       TLSConfig `mapstructure:"tls"`
	DB        DBConfig  `mapstructure:"db"`
	Noise     NoiseCfg  `mapstructure:"noise"`
	Admin     AdminCfg  `mapstructure:"admin"`
	Log       LogCfg    `mapstructure:"log"`
	WebSocket WSCfg     `mapstructure:"websocket"`
}

// TLSConfig holds optional TLS settings.
type TLSConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Cert    string `mapstructure:"cert"`
	Key     string `mapstructure:"key"`
}

// DBConfig holds SQLite settings.
type DBConfig struct {
	Path string `mapstructure:"path"`
}

// NoiseCfg holds the path to the server's Noise static keypair.
type NoiseCfg struct {
	KeyPath string `mapstructure:"key_path"`
}

// AdminCfg controls the optional admin HTTP server.
type AdminCfg struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
}

// LogCfg controls logging behaviour.
type LogCfg struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// WSCfg controls WebSocket tuning.
type WSCfg struct {
	MaxMessageBytes int64         `mapstructure:"max_message_bytes"`
	PingInterval    time.Duration `mapstructure:"ping_interval"`
	Deadline        time.Duration `mapstructure:"deadline"`
}

// Addr returns the relay server listen address.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// AdminAddr returns the admin server listen address.
func (c *Config) AdminAddr() string {
	return fmt.Sprintf("%s:%d", c.Admin.Host, c.Admin.Port)
}

// Load reads configuration from file, environment, and CLI flags.
// cfgFile may be empty — in that case Viper searches standard paths.
func Load(cfgFile string, flags *pflag.FlagSet) (*Config, error) {
	v := viper.New()

	if flags != nil {
		if err := v.BindPFlags(flags); err != nil {
			return nil, fmt.Errorf("binding flags: %w", err)
		}
	}

	// Environment variable overrides: RELAYLY_HOST, RELAYLY_PORT, etc.
	v.SetEnvPrefix("RELAYLY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Defaults
	setDefaults(v)

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("relayly")
		v.SetConfigType("yaml")
		v.AddConfigPath("./config")
		v.AddConfigPath("$HOME/.relayly")
		v.AddConfigPath(".")
	}

	// Read primary config (not fatal if missing — defaults apply)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	// Merge local override file (relayly.local.yaml) if present
	v.SetConfigName("relayly.local")
	_ = v.MergeInConfig() // silently ignore if not found

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, validate(cfg)
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("host", "0.0.0.0")
	v.SetDefault("port", 8080)
	v.SetDefault("tls.enabled", false)
	v.SetDefault("db.path", "./data/relayly.db")
	v.SetDefault("noise.key_path", "./data/server.noise.key")
	v.SetDefault("admin.enabled", true)
	v.SetDefault("admin.host", "127.0.0.1")
	v.SetDefault("admin.port", 8081)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")
	v.SetDefault("websocket.max_message_bytes", 65536)
	v.SetDefault("websocket.ping_interval", 30*time.Second)
	v.SetDefault("websocket.deadline", 60*time.Second)
}

func validate(cfg *Config) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("invalid port: %d", cfg.Port)
	}
	if cfg.TLS.Enabled && (cfg.TLS.Cert == "" || cfg.TLS.Key == "") {
		return fmt.Errorf("tls.enabled=true requires tls.cert and tls.key")
	}
	if cfg.DB.Path == "" {
		return fmt.Errorf("db.path must not be empty")
	}
	return nil
}
