package network

import (
	"fmt"
	"os/exec"
	"runtime"
)

func ConfigureFirewall() error {
	switch runtime.GOOS {
	case "linux":
		return configureLinuxFirewall()
	case "windows":
		return configureWindowsFirewall()
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func configureLinuxFirewall() error {
	commands := []string{
		"iptables -F",
		"iptables -P INPUT DROP",
		"iptables -P OUTPUT DROP",
		"iptables -A INPUT -i lo -j ACCEPT",
		"iptables -A OUTPUT -o lo -j ACCEPT",
		"iptables -A OUTPUT -d 127.0.0.1/8 -j ACCEPT",
		"iptables -A OUTPUT -m owner --uid-owner debian-tor -j ACCEPT",
		"iptables -A INPUT -m state --state ESTABLISHED,RELATED -j ACCEPT",
	}

	for _, cmd := range commands {
		args := []string{"-c", cmd}
		if err := exec.Command("bash", args...).Run(); err != nil {
			return fmt.Errorf("firewall command failed: %s: %v", cmd, err)
		}
	}
	return nil
}

func configureWindowsFirewall() error {
	commands := [][]string{
		{"netsh", "advfirewall", "set", "allprofiles", "state", "on"},
		{"netsh", "advfirewall", "set", "allprofiles", "firewallpolicy", "blockinbound,blockoutbound"},
		{"netsh", "advfirewall", "firewall", "add", "rule", "name=TorTraffic", "dir=out", "action=allow", "program=%ProgramFiles%\\Tor\\tor.exe"},
	}

	for _, cmd := range commands {
		if err := exec.Command(cmd[0], cmd[1:]...).Run(); err != nil {
			return fmt.Errorf("firewall command failed: %v: %v", cmd, err)
		}
	}
	return nil
}
