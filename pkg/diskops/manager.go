package diskops

import (
	"context"
	"log"
)

// Manager coordinates drive detection and mounting.
type Manager struct {
	Detector *Detector
	Mounter  *Mounter
}

// NewManager creates a disk operations manager.
func NewManager(mountBase string) *Manager {
	m := &Manager{
		Mounter: NewMounter(mountBase),
	}
	m.Detector = NewDetector(m.handleEvent)
	return m
}

// Start begins monitoring for drive events.
func (m *Manager) Start(ctx context.Context) {
	m.Detector.Start(ctx)
	log.Println("[diskops] manager started")
}

// Stop halts all disk operations monitoring.
func (m *Manager) Stop() {
	m.Detector.Stop()
}

func (m *Manager) handleEvent(event DriveEvent) {
	switch event.Action {
	case "add":
		if _, err := m.Mounter.Mount(event); err != nil {
			log.Printf("[diskops] auto-mount failed for %s: %v", event.Device, err)
		}
	case "remove":
		m.Mounter.Remove(event.UUID)
	}
}
