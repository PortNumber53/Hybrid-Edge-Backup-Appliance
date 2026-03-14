# **Product Requirements Document: Hybrid-Edge Backup Appliance**

## **1\. Executive Summary**

**Objective:** Deliver a zero-configuration, plug-and-play backup appliance targeted at non-technical users. The system allows users to repurpose old hardware (via a bootable USB) or purchase a pre-configured thin client to create a robust local backup server, with an optional premium cloud-sync tier.

**Target Audience:** Everyday consumers who need reliable data protection but lack the technical skills to manage NAS devices (like Synology), configure network shares, or troubleshoot Docker containers.

**Value Proposition:** "Enterprise-grade backup that just works." Total privacy via local storage, absolute simplicity in UX, and seamless "invisible" updates.

---

## **2\. System Architecture & Tech Stack**

| Layer | Component | Description |
| :---- | :---- | :---- |
| **Appliance OS** | Arch Linux (Minimal) | Read-only/immutable root file system for stability. |
| **Local Agent** | Go Daemon | Systemd service managing network state, USB udev events, and Docker. |
| **Application** | Docker Compose | Isolated environment running the actual backup/sync logic. |
| **Web UI** | Vite \+ React/Vue | Clean, intuitive dashboard served locally and mirrored to the cloud. |
| **Cloud Backend** | Cloudflare Workers \+ D1 | Global control plane, user auth, config syncing, and device discovery. |

---

## **3\. Core Workflows**

### **3.1 Provisioning & Device Discovery**

**Goal:** Connect the headless appliance to the user's home network and link it to their account.

1. **Boot:** The Arch Linux system boots. The Go Agent checks for internet access.  
2. **Network Fallback:** If no Ethernet/WiFi is found, Go triggers NetworkManager to broadcast a temporary Setup WiFi hotspot.  
3. **Captive Portal:** User connects via phone, inputs their home WiFi credentials into a local Vite UI, and the appliance connects to the local LAN.  
4. **Cloud Claiming:** The Go Agent securely pairs with the Cloudflare Worker backend. The user logs into the main website and "claims" their device using a secure PIN.

   ### **3.2 Storage Management (The "Plug & Play" UX)**

**Goal:** Completely abstract Linux filesystem management from the user.

1. **Hot-plug Event:** User plugs in an external USB hard drive.  
2. **Agent Intercept:** The Go Agent listens for kernel udev events.  
3. **UUID Mounting:** The storage manager must reliably distinguish between multiple identical devices connected simultaneously—such as a user plugging in a stack of four 4.00 TB WD My Passport drives—by mounting strictly via partition UUID instead of volatile /dev/sdX paths.  
4. **UI Feedback:** The Vite UI immediately displays a user-friendly toast notification (e.g., "New 4TB Drive Ready") and asks what data should be backed up to it.  
5. **Safe Ejection:** Users click a "Safe to Unplug" button in the UI. The Go agent flushes I/O buffers (sync) and executes a clean umount to prevent data corruption.

   ### **3.3 The Backup & Sync Engine**

**Goal:** Execute local backups and upsell cloud storage.

1. **Local Execution:** The Dockerized application handles the heavy lifting of copying files from the user's network/devices to the local USB drives.  
2. **Configuration Sync:** Backup schedules and target manifests (stored as JSON) are continuously synced to the Cloudflare D1 database. If the physical box dies, restoring the configuration to a new box takes seconds.  
3. **Premium Cloud Uplink ($15/TB/month):** For subscribed users, the Go agent securely encrypts selected high-priority files locally and pipes them to an S3-compatible cloud storage bucket.

   ### **3.4 Over-The-Air (OTA) Updates**

**Goal:** Push new features without bricking the user's appliance.

1. **Fetch:** The Go Agent periodically queries the Cloudflare Worker for updates.  
2. **Pre-flight:** If a new docker-compose.yml is available, the agent downloads it, verifies the checksum, and pulls the required Docker images in the background.  
3. **Blue-Green Swap:** The agent renames the compose files and issues docker-compose up \-d.  
4. **Health Verification:** The agent monitors the container's /health endpoint. If it fails to respond within the timeout window, the agent automatically executes a rollback to the previous compose file and logs a telemetry error to the cloud.  
   ---

   ## **4\. Technical Rationale**

* **Arch Linux \+ Go:** Provides a ridiculously small footprint, incredibly fast boot times, and raw access to Linux kernel events (like udev and D-Bus for networking) without the bloat of a desktop environment.  
* **Docker Compose:** Decouples the OS from the application logic. The OS rarely needs updating, minimizing the risk of a system-level crash, while the backup software can iterate quickly.  
* **Cloudflare Workers:** Offers ultra-low latency, globally distributed edge compute for the discovery and configuration APIs, keeping server costs near zero while scaling infinitely.  
* 

