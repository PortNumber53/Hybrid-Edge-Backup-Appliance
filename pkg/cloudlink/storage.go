package cloudlink

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// S3Uploader handles encrypted uploads to S3-compatible cloud storage
// for premium tier users ($15/TB/month).
type S3Uploader struct {
	endpoint  string
	bucket    string
	accessKey string
	secretKey string
}

// NewS3Uploader creates an uploader for cloud backup sync.
func NewS3Uploader(endpoint, bucket, accessKey, secretKey string) *S3Uploader {
	return &S3Uploader{
		endpoint:  endpoint,
		bucket:    bucket,
		accessKey: accessKey,
		secretKey: secretKey,
	}
}

// UploadDirectory syncs a local directory to the S3 bucket using rclone.
// Files are encrypted locally before upload.
func (u *S3Uploader) UploadDirectory(ctx context.Context, localPath, remotePath string) error {
	if u.endpoint == "" || u.bucket == "" {
		return fmt.Errorf("s3 uploader not configured")
	}

	remote := fmt.Sprintf(":s3:%s/%s", u.bucket, remotePath)

	args := []string{
		"sync", localPath, remote,
		"--s3-provider", "Other",
		"--s3-endpoint", u.endpoint,
		"--crypt-remote", remote,
		"--transfers", "4",
		"--checkers", "8",
		"--log-level", "INFO",
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	cmd.Env = append(os.Environ(),
		"RCLONE_S3_ACCESS_KEY_ID="+u.accessKey,
		"RCLONE_S3_SECRET_ACCESS_KEY="+u.secretKey,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rclone sync failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	log.Printf("[cloudlink] uploaded %s to %s/%s", localPath, u.bucket, remotePath)
	return nil
}

// IsConfigured returns true if S3 credentials are set.
func (u *S3Uploader) IsConfigured() bool {
	return u.endpoint != "" && u.bucket != "" && u.accessKey != "" && u.secretKey != ""
}
