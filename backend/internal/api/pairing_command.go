package api

import (
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

const defaultPairingPort = "3000"

var listInterfaceAddrs = net.InterfaceAddrs

func inferPairingServerURL(configured string, req *http.Request) string {
	if server, ok := configuredPublicServerURL(configured); ok {
		return server
	}

	scheme := pairingScheme(req)
	port := pairingPort(req)
	if ip, ok := detectPairingIP(); ok {
		return scheme + "://" + net.JoinHostPort(ip, port)
	}

	return scheme + "://localhost:" + port
}

func configuredPublicServerURL(configured string) (string, bool) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return "", false
	}

	parsed, err := url.Parse(configured)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		log.Printf("ignoring invalid publicServerURL %q; falling back to automatic pairing address", configured)
		return "", false
	}

	return parsed.String(), true
}

func pairingScheme(req *http.Request) string {
	if req != nil && req.TLS != nil {
		return "https"
	}
	return "http"
}

func pairingPort(req *http.Request) string {
	if req == nil {
		return defaultPairingPort
	}

	host := strings.TrimSpace(req.Host)
	if host == "" {
		return defaultPairingPort
	}

	_, port, err := net.SplitHostPort(host)
	if err == nil && strings.TrimSpace(port) != "" {
		return port
	}

	return defaultPairingPort
}

func detectPairingIP() (string, bool) {
	addrs, err := listInterfaceAddrs()
	if err != nil {
		return "", false
	}

	var firstNonLoopback string
	for _, addr := range addrs {
		ip := extractIPv4(addr)
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			continue
		}

		addr4, ok := netip.AddrFromSlice(ip.To4())
		if !ok {
			continue
		}
		if addr4.IsPrivate() {
			return addr4.String(), true
		}
		if firstNonLoopback == "" {
			firstNonLoopback = addr4.String()
		}
	}

	if firstNonLoopback != "" {
		return firstNonLoopback, true
	}
	return "", false
}

func extractIPv4(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP.To4()
	case *net.IPAddr:
		return value.IP.To4()
	default:
		return nil
	}
}
