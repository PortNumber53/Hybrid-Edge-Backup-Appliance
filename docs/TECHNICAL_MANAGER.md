## **1\. System Overview**

The Go service acts as the core Local Agent on the Arch Linux appliance. It operates as a privileged system daemon (systemd service) responsible for three primary domains: hardware management (USB storage), container orchestration (Docker Compose lifecycle), and cloud communication (Cloudflare Workers API).

---

## **2\. Core Go Subsystems**

To maintain a clean codebase, the Go agent should be divided into distinct packages.

### **pkg/diskops (Hardware & Storage Management)**

This module handles the physical storage layer, interacting directly with the Arch Linux host.

* **Device Detection:** Uses a library like github.com/jochenvg/go-udev to listen for kernel uevents when a user plugs in a drive.  
* **Mounting Strategy:** Instead of relying on /dev/sdX which can change, the service must read the partition UUID via lsblk (using exec.Command).  
* **Real-World Handling:** When managing consumer-grade hardware—such as standard 4TB WD My Passport external drives—the agent must reliably handle UUID mapping to prevent mount point shuffling upon reboot or if multiple identical drives are connected.  
* **Safe Eject:** Exposes an API endpoint to the local Vite UI that safely flushes I/O buffers (sync) and executes umount before telling the user it is safe to remove the drive.

### **pkg/orchestrator (Docker Lifecycle Management)**

This package manages the backup application itself, ensuring the "Blue-Green" update process is flawless for non-savvy users.

* **API Client:** Uses github.com/docker/docker/client to interact with the Docker socket.  
* **Health Monitoring:** Continuously polls the containers' health status. If a container crashes repeatedly, it triggers an alert payload to the cloud backend.

### **pkg/cloudlink (API & Telemetry)**

Handles all outbound communication to your Cloudflare Workers deployment.

* **Manifest Sync:** Periodically pulls down the user's .json backup configuration and pushes local execution logs up to the D1 database.  
* **Cloud Storage Tunneling:** For users on the $15/TB/month plan, this module wraps rclone or uses the AWS SDK for Go to securely encrypt and pipe local data to your S3-compatible backend.

---

## **3\. The Update & Swap Mechanism**

The most critical workflow is updating the underlying application without bricking the headless appliance. This process is handled entirely by pkg/orchestrator.

| Step | Action | Go Implementation Detail |
| :---- | :---- | :---- |
| **1\. Fetch** | Download new configuration | http.Get fetches docker-compose.yml and a SHA256 checksum from Cloudflare. Validates hash before proceeding. |
| **2\. Dry-Run** | Verify syntax | Executes docker-compose \-f new.yml config. If exit code \!= 0, abort and log error. |
| **3\. Pre-Pull** | Download images | Executes docker-compose \-f new.yml pull to ensure images exist locally before tearing down the old system. |
| **4\. Swap** | Restart containers | os.Rename(old, backup) \-\> os.Rename(new, active) \-\> docker-compose up \-d \--remove-orphans. |
| **5\. Verify** | Health check | Polls Docker API for 120 seconds. If status is not healthy, triggers Step 6\. |
| **6\. Rollback** | Revert on failure | os.Rename(backup, active) \-\> docker-compose up \-d. Pushes failure telemetry to the cloud. |

---

## **4\. Security & Permissions**

Because this is a hardware appliance, the Go binary requires specific privileges.

* **Execution:** Runs as root via a systemd service file (/etc/systemd/system/backup-agent.service).  
* **Filesystem Access:** Needs read/write access to /mnt or /media for handling the USB drives, and /var/run/docker.sock to manage containers.  
* **Local API Security:** The local HTTP server (serving the Vite UI) should bind only to local interfaces (127.0.0.1 and the local LAN IP) to prevent external access. CORS should be strictly configured to only accept requests from your authorized local domains/IPs.

