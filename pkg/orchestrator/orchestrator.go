// Package orchestrator manages Docker Compose lifecycle and blue-green deployments.
package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	activeFile = "docker-compose.yml"
	backupFile = "docker-compose.yml.backup"
	newFile    = "docker-compose.yml.new"
)

// Orchestrator manages the Docker Compose application lifecycle.
type Orchestrator struct {
	composeDir    string
	healthTimeout time.Duration
}

// New creates a new Orchestrator.
func New(composeDir string, healthTimeout time.Duration) *Orchestrator {
	return &Orchestrator{
		composeDir:    composeDir,
		healthTimeout: healthTimeout,
	}
}

// composePath returns the full path for a compose filename.
func (o *Orchestrator) composePath(name string) string {
	return filepath.Join(o.composeDir, name)
}

// Start brings up the current active compose stack.
func (o *Orchestrator) Start(ctx context.Context) error {
	path := o.composePath(activeFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Println("[orchestrator] no active compose file found, skipping initial start")
		return nil
	}

	log.Println("[orchestrator] starting compose stack")
	return o.composeUp(ctx)
}

// Stop tears down the compose stack.
func (o *Orchestrator) Stop(ctx context.Context) error {
	log.Println("[orchestrator] stopping compose stack")
	return o.composeDown(ctx)
}

// Update performs a blue-green deployment with the given compose YAML content.
// It follows the 6-step process: validate, dry-run, pre-pull, swap, verify, rollback-on-failure.
func (o *Orchestrator) Update(ctx context.Context, composeYAML []byte) error {
	newPath := o.composePath(newFile)
	activePath := o.composePath(activeFile)
	backupPath := o.composePath(backupFile)

	// Step 1: Write new compose file.
	if err := os.WriteFile(newPath, composeYAML, 0o644); err != nil {
		return fmt.Errorf("writing new compose file: %w", err)
	}
	log.Println("[orchestrator] update: new compose file written")

	// Step 2: Dry-run validation.
	if err := o.composeDryRun(ctx, newPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("compose dry-run failed: %w", err)
	}
	log.Println("[orchestrator] update: dry-run passed")

	// Step 3: Pre-pull images.
	if err := o.composePull(ctx, newPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("compose pre-pull failed: %w", err)
	}
	log.Println("[orchestrator] update: images pre-pulled")

	// Step 4: Swap files and restart.
	if _, err := os.Stat(activePath); err == nil {
		if err := os.Rename(activePath, backupPath); err != nil {
			_ = os.Remove(newPath)
			return fmt.Errorf("backing up active compose file: %w", err)
		}
	}
	if err := os.Rename(newPath, activePath); err != nil {
		// Try to restore backup.
		_ = os.Rename(backupPath, activePath)
		return fmt.Errorf("activating new compose file: %w", err)
	}

	if err := o.composeUp(ctx); err != nil {
		log.Printf("[orchestrator] update: compose up failed, rolling back: %v", err)
		if rbErr := o.rollback(ctx, activePath, backupPath); rbErr != nil {
			return fmt.Errorf("rollback failed after compose up error: %w (original: %v)", rbErr, err)
		}
		return fmt.Errorf("update rolled back: compose up failed: %w", err)
	}
	log.Println("[orchestrator] update: new stack started")

	// Step 5: Health verification.
	if err := o.waitForHealthy(ctx); err != nil {
		log.Printf("[orchestrator] update: health check failed, rolling back: %v", err)
		if rbErr := o.rollback(ctx, activePath, backupPath); rbErr != nil {
			return fmt.Errorf("rollback failed after health check error: %w (original: %v)", rbErr, err)
		}
		return fmt.Errorf("update rolled back: health check failed: %w", err)
	}
	log.Println("[orchestrator] update: health check passed")

	// Clean up backup on success.
	_ = os.Remove(backupPath)
	return nil
}

// rollback reverts to the backup compose file.
func (o *Orchestrator) rollback(ctx context.Context, activePath, backupPath string) error {
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup to rollback to")
	}

	_ = os.Remove(activePath)
	if err := os.Rename(backupPath, activePath); err != nil {
		return fmt.Errorf("rollback rename failed: %w", err)
	}

	if err := o.composeUp(ctx); err != nil {
		return fmt.Errorf("rollback compose up failed: %w", err)
	}

	log.Println("[orchestrator] rollback completed successfully")
	return nil
}

func (o *Orchestrator) composeUp(ctx context.Context) error {
	return o.runCompose(ctx, o.composePath(activeFile), "up", "-d", "--remove-orphans")
}

func (o *Orchestrator) composeDown(ctx context.Context) error {
	return o.runCompose(ctx, o.composePath(activeFile), "down")
}

func (o *Orchestrator) composeDryRun(ctx context.Context, file string) error {
	return o.runCompose(ctx, file, "config")
}

func (o *Orchestrator) composePull(ctx context.Context, file string) error {
	return o.runCompose(ctx, file, "pull")
}

func (o *Orchestrator) runCompose(ctx context.Context, file string, args ...string) error {
	cmdArgs := append([]string{"-f", file}, args...)
	cmd := exec.CommandContext(ctx, "docker-compose", cmdArgs...)
	cmd.Dir = o.composeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// waitForHealthy polls container health for up to the configured timeout.
func (o *Orchestrator) waitForHealthy(ctx context.Context) error {
	deadline := time.After(o.healthTimeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("health check timed out after %s", o.healthTimeout)
		case <-ticker.C:
			healthy, err := o.checkHealth(ctx)
			if err != nil {
				log.Printf("[orchestrator] health probe error: %v", err)
				continue
			}
			if healthy {
				return nil
			}
		}
	}
}

// checkHealth inspects running containers for health status.
func (o *Orchestrator) checkHealth(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps",
		"--filter", "label=com.docker.compose.project",
		"--format", "{{.Status}}")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return false, fmt.Errorf("no containers running")
	}

	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "unhealthy") {
			return false, nil
		}
		// If container has a health check, it must say "healthy".
		if strings.Contains(lower, "health:") && !strings.Contains(lower, "healthy") {
			return false, nil
		}
	}
	return true, nil
}

// IsRunning returns true if any compose containers are currently running.
func (o *Orchestrator) IsRunning(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker-compose",
		"-f", o.composePath(activeFile), "ps", "-q")
	out, _ := cmd.Output()
	return len(strings.TrimSpace(string(out))) > 0
}
