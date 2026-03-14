// backup-agent is the main daemon for the Hybrid-Edge Backup Appliance.
// It runs as a systemd service managing USB storage, Docker containers,
// and cloud synchronization.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/api"
	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/cloudlink"
	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/config"
	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/diskops"
	"github.com/PortNumber53/Hybrid-Edge-Backup-Appliance/pkg/orchestrator"
)

const version = "0.1.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("backup-agent %s starting", version)

	// Load configuration.
	cfgPath := config.DefaultConfigPath
	if p := os.Getenv("BACKUP_AGENT_CONFIG"); p != "" {
		cfgPath = p
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize disk operations (USB detection + UUID mounting).
	diskMgr := diskops.NewManager(cfg.MountBase)
	diskMgr.Start(ctx)

	// Initialize Docker orchestrator.
	orch := orchestrator.New(cfg.ComposeDir, cfg.HealthTimeout.Duration)
	if err := orch.Start(ctx); err != nil {
		log.Printf("warning: initial compose start failed: %v", err)
	}

	// Initialize cloud link.
	cloud := cloudlink.NewClient(cfg.CloudEndpoint, cfg.DeviceID, cfg.DeviceSecret)
	cloud.StartPeriodicSync(ctx, cfg.UpdateInterval.Duration, func() cloudlink.DeviceStatus {
		mounts := diskMgr.Mounter.ListMounts()
		uuids := make([]string, len(mounts))
		for i, m := range mounts {
			uuids[i] = m.UUID
		}
		return cloudlink.DeviceStatus{
			Drives:  uuids,
			Healthy: orch.IsRunning(ctx),
			Version: version,
		}
	})

	// Start periodic update checker.
	go runUpdateLoop(ctx, cloud, orch, cfg.UpdateInterval.Duration)

	// Start local HTTP API server.
	srv := api.NewServer(cfg.ListenAddr, diskMgr, orch)
	if err := srv.Start(); err != nil {
		log.Fatalf("failed to start API server: %v", err)
	}

	log.Println("backup-agent is running")

	// Wait for shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("received signal %s, shutting down", sig)

	// Graceful shutdown.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Stop(shutdownCtx); err != nil {
		log.Printf("api server shutdown error: %v", err)
	}
	diskMgr.Stop()
	log.Println("backup-agent stopped")
}

// runUpdateLoop periodically checks for and applies OTA updates.
func runUpdateLoop(ctx context.Context, cloud *cloudlink.Client, orch *orchestrator.Orchestrator, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := cloud.CheckForUpdate(ctx)
			if err != nil {
				log.Printf("[update] check failed: %v", err)
				continue
			}
			if !info.Available {
				continue
			}

			log.Printf("[update] new version available: %s", info.Version)
			if err := orch.Update(ctx, info.ComposeYAML); err != nil {
				log.Printf("[update] failed: %v", err)
				_ = cloud.ReportUpdateFailure(ctx, info.Version, err.Error())
				continue
			}
			log.Printf("[update] successfully updated to %s", info.Version)
		}
	}
}
