## **1\. Overview**

We will use the official `archiso` tool to create a custom, minimal Live ISO based on the `releng` profile. This ISO will be stripped of unnecessary desktop components and pre-loaded with Docker, NetworkManager, and your custom Go Agent daemon.

Because the live environment runs entirely in RAM (overlayfs), the root OS remains immutable. Any misconfiguration or corruption can be fixed by simply rebooting the appliance.

---

## **2\. Setup & Workspace Preparation**

First, ensure the build tools are installed on your build machine, then copy the default `releng` profile to your home directory to begin customization.

Bash  
\[grimlock@homi \~\]$ sudo pacman \-S archiso qemu-full  
\[grimlock@homi \~\]$ cp \-r /usr/share/archiso/configs/releng/ \~/archlive  
\[grimlock@homi \~\]$ cd \~/archlive

---

## **3\. Package Selection (`packages.x86_64`)**

Open `~/archlive/packages.x86_64` and strip out graphical utilities (like `memtest86+`, `brltty`, etc.). You want a headless, network-ready core.

Ensure the following essential packages are included in the list:

Plaintext  
\# Networking & Discovery  
networkmanager  
avahi  
nss-mdns  
curl

\# Container Runtime  
docker  
docker-compose

\# Hardware & Storage  
udisks2  
ntfs-3g  
smartmontools

\# Application Dependencies  
jq  
tar

---

## **4\. Injecting the Application (`airootfs`)**

The `airootfs` (Arch ISO Root Filesystem) directory acts as the overlay for your custom image. Anything you place in `~/archlive/airootfs/` will be mirrored to the root `/` of the final booted OS.

### **4.1. The Go Agent Binary**

Compile your Go service for standard Linux `x86_64` and place it in the `airootfs/usr/local/bin/` directory.

Bash  
\[grimlock@homi archlive\]$ mkdir \-p airootfs/usr/local/bin  
\[grimlock@homi archlive\]$ cp /path/to/your/compiled/backup-agent airootfs/usr/local/bin/  
\[grimlock@homi archlive\]$ chmod \+x airootfs/usr/local/bin/backup-agent

### **4.2. Base Docker Compose File**

Place the initial (or blank) `docker-compose.yml` that the Go agent will use as a starting point.

Bash  
\[grimlock@homi archlive\]$ mkdir \-p airootfs/opt/backup/  
\[grimlock@homi archlive\]$ cp /path/to/base/docker-compose.yml airootfs/opt/backup/

---

## **5\. Systemd Configuration (Auto-Start)**

The appliance must auto-start all services without user login. We achieve this by creating systemd symlinks within the `airootfs` environment.

### **5.1. The Go Agent Service**

Create the service file at `~/archlive/airootfs/etc/systemd/system/backup-agent.service`:

Ini, TOML  
\[Unit\]  
Description=Hybrid-Edge Backup Agent  
After=docker.service network-online.target  
Wants=network-online.target

\[Service\]  
Type=simple  
ExecStart=/usr/local/bin/backup-agent  
Restart=always  
RestartSec=5

\[Install\]  
WantedBy=multi-user.target

### **5.2. Enabling the Services**

To ensure `NetworkManager`, `docker`, and your `backup-agent` start automatically on boot, create the required systemd symlinks manually within the `airootfs` tree:

Bash  
\[grimlock@homi archlive\]$ ln \-s /usr/lib/systemd/system/NetworkManager.service airootfs/etc/systemd/system/multi-user.target.wants/NetworkManager.service  
\[grimlock@homi archlive\]$ ln \-s /usr/lib/systemd/system/docker.service airootfs/etc/systemd/system/multi-user.target.wants/docker.service  
\[grimlock@homi archlive\]$ ln \-s /etc/systemd/system/backup-agent.service airootfs/etc/systemd/system/multi-user.target.wants/backup-agent.service

---

## **6\. Build the ISO**

With the packages defined and the filesystem pre-populated, use `mkarchiso` to compile the image. Note: This requires root privileges and takes a few minutes depending on your internet connection.

Bash  
\[grimlock@homi archlive\]$ sudo mkarchiso \-v \-w /tmp/archiso-tmp \-o ./out ./

Once completed, your custom bootable image will be located at `~/archlive/out/archlinux-backup-appliance-*.iso`.

---

## **7\. Testing the Image Locally**

Before flashing this to a physical USB drive and plugging it into a spare PC, you can rapidly test the boot process and service initialization using QEMU:

Bash  
\[grimlock@homi archlive\]$ qemu-system-x86\_64 \-m 2048 \-enable-kvm \\  
  \-cdrom out/archlinux-backup-appliance-\*.iso \\  
  \-device e1000,netdev=net0 \\  
  \-netdev user,id=net0,hostfwd=tcp::8080-:80

*(This command allocates 2GB of RAM to the VM and forwards port 8080 on your host to port 80 on the appliance, allowing you to test the local Vite UI via your browser).*

---

