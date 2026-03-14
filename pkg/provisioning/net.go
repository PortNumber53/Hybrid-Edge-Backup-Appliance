package provisioning

import "net"

// getMACAddresses returns the MAC addresses of all non-loopback network interfaces.
func getMACAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var macs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		mac := iface.HardwareAddr.String()
		if mac != "" {
			macs = append(macs, mac)
		}
	}
	return macs
}
