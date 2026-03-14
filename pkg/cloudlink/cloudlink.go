// Package cloudlink handles communication with the Cloudflare Workers backend.
package cloudlink

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Client communicates with the Cloudflare Workers control plane.
type Client struct {
	endpoint   string
	deviceID   string
	secret     string
	httpClient *http.Client
}

// NewClient creates a new cloud link client.
func NewClient(endpoint, deviceID, secret string) *Client {
	return &Client{
		endpoint: endpoint,
		deviceID: deviceID,
		secret:   secret,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// DeviceStatus is pushed to the cloud periodically.
type DeviceStatus struct {
	DeviceID  string   `json:"device_id"`
	Uptime    int64    `json:"uptime_seconds"`
	Drives    []string `json:"drives"`
	Healthy   bool     `json:"healthy"`
	Version   string   `json:"version"`
	Timestamp int64    `json:"timestamp"`
}

// UpdateInfo is returned when a new update is available.
type UpdateInfo struct {
	Available    bool   `json:"available"`
	ComposeYAML  []byte `json:"compose_yaml,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	Version      string `json:"version,omitempty"`
	ReleaseNotes string `json:"release_notes,omitempty"`
}

// ManifestConfig represents the user's backup configuration synced from the cloud.
type ManifestConfig struct {
	Schedules []BackupSchedule `json:"schedules"`
	Targets   []BackupTarget   `json:"targets"`
	UpdatedAt int64            `json:"updated_at"`
}

// BackupSchedule defines when a backup should run.
type BackupSchedule struct {
	ID       string `json:"id"`
	Cron     string `json:"cron"`
	TargetID string `json:"target_id"`
	Enabled  bool   `json:"enabled"`
}

// BackupTarget defines what should be backed up and where.
type BackupTarget struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	DestUUID string `json:"dest_uuid"`
}

// PushStatus sends the device status to the cloud backend.
func (c *Client) PushStatus(ctx context.Context, status DeviceStatus) error {
	status.DeviceID = c.deviceID
	status.Timestamp = time.Now().Unix()
	return c.post(ctx, "/api/devices/status", status)
}

// CheckForUpdate queries the cloud for available updates.
func (c *Client) CheckForUpdate(ctx context.Context) (*UpdateInfo, error) {
	body, err := c.get(ctx, "/api/devices/updates")
	if err != nil {
		return nil, err
	}

	var info UpdateInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parsing update response: %w", err)
	}

	// Verify checksum if update is available.
	if info.Available && len(info.ComposeYAML) > 0 {
		hash := sha256.Sum256(info.ComposeYAML)
		actual := hex.EncodeToString(hash[:])
		if actual != info.SHA256 {
			return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", info.SHA256, actual)
		}
	}

	return &info, nil
}

// FetchManifest retrieves the user's backup configuration.
func (c *Client) FetchManifest(ctx context.Context) (*ManifestConfig, error) {
	body, err := c.get(ctx, "/api/devices/manifest")
	if err != nil {
		return nil, err
	}

	var manifest ManifestConfig
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return &manifest, nil
}

// PushLogs sends execution logs to the cloud.
func (c *Client) PushLogs(ctx context.Context, logs interface{}) error {
	return c.post(ctx, "/api/devices/logs", logs)
}

// ReportUpdateFailure reports an update rollback event.
func (c *Client) ReportUpdateFailure(ctx context.Context, version string, reason string) error {
	payload := map[string]string{
		"device_id": c.deviceID,
		"version":   version,
		"reason":    reason,
	}
	return c.post(ctx, "/api/devices/update-failure", payload)
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	c.addAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloud request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloud returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) post(ctx context.Context, path string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.addAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloud request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("cloud returned status %d, but reading body failed: %w", resp.StatusCode, readErr)
		}
		return fmt.Errorf("cloud returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) addAuth(req *http.Request) {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(ts + "\n" + req.Method + "\n" + req.URL.Path))
	sig := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("X-Device-ID", c.deviceID)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", sig)
}

// StartPeriodicSync begins periodic status reporting and manifest syncing.
func (c *Client) StartPeriodicSync(ctx context.Context, interval time.Duration, statusFn func() DeviceStatus) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				status := statusFn()
				if err := c.PushStatus(ctx, status); err != nil {
					log.Printf("[cloudlink] status push failed: %v", err)
				}
			}
		}
	}()
	log.Printf("[cloudlink] periodic sync started (interval=%s)", interval)
}
