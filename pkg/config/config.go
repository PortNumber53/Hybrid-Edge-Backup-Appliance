// Package config provides application configuration for the backup agent.
package config

import (
	"encoding/json"
	"os"
	"time"
)

const (
	DefaultMountBase     = "/mnt/backup"
	DefaultComposeDir    = "/opt/backup"
	DefaultComposeFile   = "docker-compose.yml"
	DefaultListenAddr    = "0.0.0.0:80"
	DefaultHealthTimeout = 120 * time.Second
	DefaultCheckInterval = 5 * time.Minute
	DefaultConfigPath    = "/etc/backup-agent/config.json"
)

// Config holds the full agent configuration.
type Config struct {
	MountBase      string `json:"mount_base"`
	ComposeDir     string `json:"compose_dir"`
	ComposeFile    string `json:"compose_file"`
	ListenAddr     string `json:"listen_addr"`
	CloudEndpoint  string `json:"cloud_endpoint"`
	DeviceID       string `json:"device_id"`
	DeviceSecret   string `json:"device_secret"`
	HealthTimeout  Duration `json:"health_timeout"`
	UpdateInterval Duration `json:"update_interval"`
	S3Endpoint     string `json:"s3_endpoint"`
	S3Bucket       string `json:"s3_bucket"`
	S3AccessKey    string `json:"s3_access_key"`
	S3SecretKey    string `json:"s3_secret_key"`
}

// Duration wraps time.Duration for JSON marshaling.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// Load reads configuration from path, falling back to defaults.
func Load(path string) (*Config, error) {
	cfg := &Config{
		MountBase:      DefaultMountBase,
		ComposeDir:     DefaultComposeDir,
		ComposeFile:    DefaultComposeFile,
		ListenAddr:     DefaultListenAddr,
		HealthTimeout:  Duration{DefaultHealthTimeout},
		UpdateInterval: Duration{DefaultCheckInterval},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
