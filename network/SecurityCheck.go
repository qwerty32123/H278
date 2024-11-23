package network

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
)

type SecurityChecker struct {
	localIPs []string
}

func NewSecurityChecker() (*SecurityChecker, error) {
	ips, err := getLocalIPs()
	if err != nil {
		return nil, err
	}
	return &SecurityChecker{localIPs: ips}, nil
}

func getLocalIPs() ([]string, error) {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				ips = append(ips, v.IP.String())
			case *net.IPAddr:
				ips = append(ips, v.IP.String())
			}
		}
	}
	return ips, nil
}

func (sc *SecurityChecker) IsLocalIP(ip string) bool {
	for _, localIP := range sc.localIPs {
		if strings.HasPrefix(ip, localIP) {
			return true
		}
	}
	return false
}

func (sc *SecurityChecker) CheckIPLeak(client *http.Client) error {
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	publicIP := string(ip)
	if sc.IsLocalIP(publicIP) {
		fmt.Printf("SECURITY ALERT: Traffic detected through local IP: %s\n", publicIP)
		os.Exit(1)
	}
	return nil
}
