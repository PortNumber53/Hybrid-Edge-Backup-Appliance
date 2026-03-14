package diskops

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// MountInfo tracks a mounted drive.
type MountInfo struct {
	UUID       string `json:"uuid"`
	Label      string `json:"label"`
	Device     string `json:"device"`
	MountPoint string `json:"mount_point"`
	FSType     string `json:"fstype"`
	SizeHuman  string `json:"size_human"`
}

// Mounter manages UUID-based mounting and unmounting of external drives.
type Mounter struct {
	baseDir string
	mu      sync.RWMutex
	mounts  map[string]*MountInfo // keyed by UUID
}

// NewMounter creates a mounter that organizes drives under baseDir.
func NewMounter(baseDir string) *Mounter {
	return &Mounter{
		baseDir: baseDir,
		mounts:  make(map[string]*MountInfo),
	}
}

// Mount mounts a device by its UUID to baseDir/<uuid>.
func (m *Mounter) Mount(event DriveEvent) (*MountInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.mounts[event.UUID]; exists {
		return m.mounts[event.UUID], nil
	}

	mountPoint := filepath.Join(m.baseDir, event.UUID)
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return nil, fmt.Errorf("creating mount point %s: %w", mountPoint, err)
	}

	// Mount by UUID for stability across reboots and device reordering.
	args := []string{"-U", event.UUID, mountPoint}
	if event.FSType == "ntfs" || event.FSType == "ntfs3" {
		args = []string{"-t", "ntfs3", "-U", event.UUID, mountPoint}
	}

	cmd := exec.Command("mount", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("mounting UUID=%s: %s: %w", event.UUID, strings.TrimSpace(string(out)), err)
	}

	info := &MountInfo{
		UUID:       event.UUID,
		Label:      event.Label,
		Device:     event.Device,
		MountPoint: mountPoint,
		FSType:     event.FSType,
		SizeHuman:  event.SizeHuman,
	}
	m.mounts[event.UUID] = info
	log.Printf("[diskops] mounted UUID=%s at %s (device=%s, size=%s)", event.UUID, mountPoint, event.Device, event.SizeHuman)
	return info, nil
}

// Unmount safely unmounts the drive identified by uuid.
func (m *Mounter) Unmount(uuid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.mounts[uuid]
	if !exists {
		return fmt.Errorf("no mounted drive with UUID=%s", uuid)
	}

	// Flush I/O buffers before unmounting.
	if err := exec.Command("sync").Run(); err != nil {
		log.Printf("[diskops] sync warning: %v", err)
	}

	if out, err := exec.Command("umount", info.MountPoint).CombinedOutput(); err != nil {
		return fmt.Errorf("unmounting %s: %s: %w", info.MountPoint, strings.TrimSpace(string(out)), err)
	}

	// Clean up empty mount directory.
	_ = os.Remove(info.MountPoint)

	delete(m.mounts, uuid)
	log.Printf("[diskops] unmounted UUID=%s from %s", uuid, info.MountPoint)
	return nil
}

// Remove handles a drive removal event (cleans up state without unmounting).
func (m *Mounter) Remove(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, exists := m.mounts[uuid]; exists {
		log.Printf("[diskops] drive removed: UUID=%s (was at %s)", uuid, info.MountPoint)
		_ = os.Remove(info.MountPoint)
		delete(m.mounts, uuid)
	}
}

// ListMounts returns a snapshot of all currently mounted drives.
func (m *Mounter) ListMounts() []MountInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MountInfo, 0, len(m.mounts))
	for _, info := range m.mounts {
		result = append(result, *info)
	}
	return result
}

// GetMount returns mount info for a specific UUID.
func (m *Mounter) GetMount(uuid string) (*MountInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info, ok := m.mounts[uuid]
	if !ok {
		return nil, false
	}
	cp := *info
	return &cp, true
}
