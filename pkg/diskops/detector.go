// Package diskops handles USB storage detection, UUID-based mounting, and safe ejection.
package diskops

import (
	"context"
	"log"
	"os/exec"
	"strings"
	"time"
)

// DriveEvent represents a detected storage device event.
type DriveEvent struct {
	Action    string // "add" or "remove"
	Device    string // e.g., "/dev/sdb1"
	UUID      string
	Label     string
	FSType    string
	SizeBytes uint64
	SizeHuman string
}

// EventHandler is called when a drive event is detected.
type EventHandler func(event DriveEvent)

// Detector watches for block device udev events via udevadm monitor.
type Detector struct {
	handler EventHandler
	cancel  context.CancelFunc
}

// NewDetector creates a new drive detector with the given event handler.
func NewDetector(handler EventHandler) *Detector {
	return &Detector{handler: handler}
}

// Start begins listening for udev block device events.
func (d *Detector) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	go d.monitor(ctx)
	log.Println("[diskops] drive detector started")
}

// Stop halts the detector.
func (d *Detector) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *Detector) monitor(ctx context.Context) {
	for {
		cmd := exec.CommandContext(ctx, "udevadm", "monitor",
			"--kernel", "--subsystem-match=block", "--property")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("[diskops] failed to start udevadm monitor: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		if err := cmd.Start(); err != nil {
			log.Printf("[diskops] udevadm start error: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		buf := make([]byte, 4096)
		var block strings.Builder
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				break
			}
			block.Write(buf[:n])
			content := block.String()

			// Each event block ends with a blank line.
			if strings.Contains(content, "\n\n") {
				events := strings.Split(content, "\n\n")
				for _, raw := range events[:len(events)-1] {
					d.parseAndDispatch(raw)
				}
				block.Reset()
				block.WriteString(events[len(events)-1])
			}
		}

		_ = cmd.Wait()

		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (d *Detector) parseAndDispatch(raw string) {
	lines := strings.Split(raw, "\n")
	var action, devpath string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ACTION=") {
			action = strings.TrimPrefix(line, "ACTION=")
		}
		if strings.HasPrefix(line, "DEVNAME=") {
			devpath = strings.TrimPrefix(line, "DEVNAME=")
		}
	}

	if action == "" || devpath == "" {
		return
	}

	// Only handle partition events (not whole disks).
	if !isPartition(devpath) {
		return
	}

	if action != "add" && action != "remove" {
		return
	}

	// Small delay to let the kernel settle.
	time.Sleep(500 * time.Millisecond)

	info := queryBlockDevice(devpath)
	event := DriveEvent{
		Action:    action,
		Device:    devpath,
		UUID:      info["UUID"],
		Label:     info["LABEL"],
		FSType:    info["FSTYPE"],
		SizeHuman: info["SIZE"],
	}

	if event.UUID == "" && action == "add" {
		log.Printf("[diskops] skipping device %s with no UUID", devpath)
		return
	}

	log.Printf("[diskops] drive event: action=%s device=%s uuid=%s label=%s size=%s",
		event.Action, event.Device, event.UUID, event.Label, event.SizeHuman)
	d.handler(event)
}

func isPartition(devpath string) bool {
	// Partitions typically end with a digit (sdb1, nvme0n1p1, etc.)
	if len(devpath) == 0 {
		return false
	}
	last := devpath[len(devpath)-1]
	return last >= '0' && last <= '9'
}

// queryBlockDevice uses lsblk to retrieve device information by device path.
func queryBlockDevice(device string) map[string]string {
	result := make(map[string]string)
	out, err := exec.Command("lsblk", "-no", "UUID,LABEL,FSTYPE,SIZE", device).Output()
	if err != nil {
		return result
	}

	fields := strings.Fields(strings.TrimSpace(string(out)))
	keys := []string{"UUID", "LABEL", "FSTYPE", "SIZE"}
	for i, key := range keys {
		if i < len(fields) {
			result[key] = fields[i]
		}
	}
	return result
}

// QueryBlockDeviceByUUID uses blkid to find the device path for a given UUID.
func QueryBlockDeviceByUUID(uuid string) (string, error) {
	out, err := exec.Command("blkid", "-U", uuid).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
