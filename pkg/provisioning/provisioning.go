// Package provisioning handles first-boot device identity setup.
// It supports three paths: USB-based provisioning, cloud self-registration,
// and offline mode with a locally generated device ID.
package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// USBProvisioningFile is the filename to look for on mounted USB drives.
	USBProvisioningFile = "provisioning.json"
	// LocalIDPrefix marks a device as offline/self-registered.
	LocalIDPrefix = "local-"
)

// Credentials holds the identity returned by any provisioning path.
type Credentials struct {
	DeviceID      string `json:"device_id"`
	DeviceSecret  string `json:"device_secret"`
	CloudEndpoint string `json:"cloud_endpoint,omitempty"`
}

// USBCredentials is the format expected in provisioning.json on a USB drive.
type USBCredentials struct {
	DeviceID      string `json:"device_id"`
	DeviceSecret  string `json:"device_secret"`
	CloudEndpoint string `json:"cloud_endpoint"`
}

// CloudRegistrationRequest is sent to the cloud to register a new device.
type CloudRegistrationRequest struct {
	Hostname string `json:"hostname"`
	MACAddrs []string `json:"mac_addrs,omitempty"`
}

// CloudRegistrationResponse is returned by the cloud registration endpoint.
type CloudRegistrationResponse struct {
	DeviceID     string `json:"device_id"`
	DeviceSecret string `json:"device_secret"`
}

// Result describes the outcome of provisioning.
type Result struct {
	Credentials Credentials
	Method      string // "usb", "cloud", or "offline"
}

// IsOffline returns true if the device is running in offline mode.
func IsOffline(deviceID string) bool {
	return strings.HasPrefix(deviceID, LocalIDPrefix)
}

// Run attempts provisioning in order: USB, cloud, then offline fallback.
// mountBase is the directory where USB drives are mounted (e.g., /mnt/backup).
// cloudEndpoint is the cloud API URL (may be empty).
func Run(ctx context.Context, mountBase, cloudEndpoint string) (*Result, error) {
	// Path 1: Check for USB provisioning file.
	creds, err := tryUSBProvisioning(mountBase)
	if err == nil {
		log.Printf("[provisioning] device provisioned via USB (device_id=%s)", creds.DeviceID)
		return &Result{Credentials: *creds, Method: "usb"}, nil
	}

	// Path 2: Cloud self-registration.
	if cloudEndpoint != "" {
		creds, err = tryCloudRegistration(ctx, cloudEndpoint)
		if err == nil {
			log.Printf("[provisioning] device registered via cloud (device_id=%s)", creds.DeviceID)
			return &Result{Credentials: *creds, Method: "cloud"}, nil
		}
		log.Printf("[provisioning] cloud registration failed: %v", err)
	}

	// Path 3: Offline mode — generate a local-only device ID.
	localID := LocalIDPrefix + uuid.New().String()
	log.Printf("[provisioning] entering offline mode (device_id=%s)", localID)
	return &Result{
		Credentials: Credentials{DeviceID: localID},
		Method:      "offline",
	}, nil
}

// tryUSBProvisioning scans mounted drives for a provisioning.json file.
func tryUSBProvisioning(mountBase string) (*Credentials, error) {
	entries, err := os.ReadDir(mountBase)
	if err != nil {
		return nil, fmt.Errorf("cannot read mount base %s: %w", mountBase, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(mountBase, entry.Name(), USBProvisioningFile)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var usb USBCredentials
		if err := json.Unmarshal(data, &usb); err != nil {
			log.Printf("[provisioning] invalid provisioning.json on %s: %v", entry.Name(), err)
			continue
		}

		if usb.DeviceID == "" || usb.DeviceSecret == "" {
			log.Printf("[provisioning] provisioning.json on %s has empty credentials, skipping", entry.Name())
			continue
		}

		return &Credentials{
			DeviceID:      usb.DeviceID,
			DeviceSecret:  usb.DeviceSecret,
			CloudEndpoint: usb.CloudEndpoint,
		}, nil
	}

	return nil, fmt.Errorf("no provisioning.json found on mounted drives")
}

// tryCloudRegistration registers this device with the cloud backend.
func tryCloudRegistration(ctx context.Context, endpoint string) (*Credentials, error) {
	hostname, _ := os.Hostname()

	reqBody := CloudRegistrationRequest{
		Hostname: hostname,
		MACAddrs: getMACAddresses(),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/devices/register", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registration request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration returned %d: %s", resp.StatusCode, string(body))
	}

	var regResp CloudRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, fmt.Errorf("parsing registration response: %w", err)
	}

	if regResp.DeviceID == "" {
		return nil, fmt.Errorf("cloud returned empty device_id")
	}

	return &Credentials{
		DeviceID:      regResp.DeviceID,
		DeviceSecret:  regResp.DeviceSecret,
		CloudEndpoint: endpoint,
	}, nil
}
