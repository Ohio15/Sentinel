// Package mdns provides mDNS service advertisement for the Sentinel server.
// This allows agents on the local network to automatically discover the server
// without requiring manual configuration of server addresses.
package mdns

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	// ServiceName is the mDNS service name for Sentinel servers
	ServiceName = "_sentinel._tcp"

	// ServiceDomain is the default domain for mDNS
	ServiceDomain = "local."

	// mDNSMulticastAddr is the standard mDNS multicast address
	mDNSMulticastAddr = "224.0.0.251:5353"

	// AdvertiseInterval is how often to send unsolicited advertisements
	AdvertiseInterval = 2 * time.Minute
)

// Advertiser broadcasts the Sentinel server presence via mDNS
type Advertiser struct {
	port       int
	hostname   string
	serverID   string
	version    string
	conn       *net.UDPConn
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// NewAdvertiser creates a new mDNS advertiser
func NewAdvertiser(port int, serverID, version string) (*Advertiser, error) {
	hostname, err := getHostname()
	if err != nil {
		hostname = "sentinel-server"
	}

	return &Advertiser{
		port:     port,
		hostname: hostname,
		serverID: serverID,
		version:  version,
	}, nil
}

// Start begins mDNS advertisement
func (a *Advertiser) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return nil
	}

	// Create UDP connection for multicast
	addr, err := net.ResolveUDPAddr("udp", mDNSMulticastAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve mDNS address: %w", err)
	}

	// Listen on all interfaces
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return fmt.Errorf("failed to create UDP listener: %w", err)
	}

	a.conn = conn
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.running = true

	// Start query responder
	a.wg.Add(1)
	go a.respondToQueries(addr)

	// Start periodic advertiser
	a.wg.Add(1)
	go a.periodicAdvertise(addr)

	log.Printf("[mDNS] Advertising Sentinel server on port %d (hostname: %s, serverID: %s)",
		a.port, a.hostname, a.serverID)

	return nil
}

// Stop stops mDNS advertisement
func (a *Advertiser) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return
	}

	a.cancel()
	a.conn.Close()
	a.wg.Wait()
	a.running = false

	log.Println("[mDNS] Stopped advertising")
}

// respondToQueries listens for mDNS queries and responds
func (a *Advertiser) respondToQueries(multicastAddr *net.UDPAddr) {
	defer a.wg.Done()

	// Create a second connection for receiving multicasts
	listenAddr, _ := net.ResolveUDPAddr("udp", ":5353")
	listenConn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		log.Printf("[mDNS] Warning: Could not listen on port 5353, query responses disabled: %v", err)
		return
	}
	defer listenConn.Close()

	buf := make([]byte, 4096)
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		listenConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := listenConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if !strings.Contains(err.Error(), "closed") {
				log.Printf("[mDNS] Read error: %v", err)
			}
			continue
		}

		// Check if this is a query for our service
		if a.isQueryForUs(buf[:n]) {
			log.Printf("[mDNS] Received query from %s, sending response", remoteAddr)
			a.sendResponse(multicastAddr)
		}
	}
}

// periodicAdvertise sends periodic mDNS announcements
func (a *Advertiser) periodicAdvertise(multicastAddr *net.UDPAddr) {
	defer a.wg.Done()

	// Send initial announcement
	a.sendResponse(multicastAddr)

	ticker := time.NewTicker(AdvertiseInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.sendResponse(multicastAddr)
		}
	}
}

// isQueryForUs checks if an mDNS query is for our service
func (a *Advertiser) isQueryForUs(data []byte) bool {
	if len(data) < 12 {
		return false
	}

	// Check if this is a query (QR bit not set)
	if data[2]&0x80 != 0 {
		return false // This is a response, not a query
	}

	// Look for "_sentinel" in the query
	dataStr := strings.ToLower(string(data))
	return strings.Contains(dataStr, "sentinel")
}

// sendResponse sends an mDNS response advertising our service
func (a *Advertiser) sendResponse(multicastAddr *net.UDPAddr) {
	response := a.buildResponse()

	_, err := a.conn.WriteToUDP(response, multicastAddr)
	if err != nil {
		log.Printf("[mDNS] Failed to send response: %v", err)
	}
}

// buildResponse creates an mDNS response packet
func (a *Advertiser) buildResponse() []byte {
	// Get local IP address
	localIP := getLocalIP()

	// Build mDNS response
	// This is a simplified mDNS response - in production, use a proper mDNS library
	response := []byte{
		0x00, 0x00, // Transaction ID
		0x84, 0x00, // Flags: Response, Authoritative
		0x00, 0x00, // Questions: 0
		0x00, 0x03, // Answer RRs: 3 (PTR, SRV, TXT)
		0x00, 0x00, // Authority RRs
		0x00, 0x01, // Additional RRs: 1 (A record)
	}

	// PTR record: _sentinel._tcp.local -> sentinel-server._sentinel._tcp.local
	serviceName := fmt.Sprintf("%s.%s.%s", a.hostname, ServiceName, ServiceDomain)
	response = append(response, a.encodeName(ServiceName+"."+ServiceDomain)...)
	response = append(response, 0x00, 0x0c) // Type: PTR
	response = append(response, 0x00, 0x01) // Class: IN
	response = append(response, 0x00, 0x00, 0x0e, 0x10) // TTL: 3600
	nameData := a.encodeName(serviceName)
	response = append(response, byte(len(nameData)>>8), byte(len(nameData))) // Data length
	response = append(response, nameData...)

	// SRV record: sentinel-server._sentinel._tcp.local -> hostname:port
	response = append(response, a.encodeName(serviceName)...)
	response = append(response, 0x00, 0x21) // Type: SRV
	response = append(response, 0x00, 0x01) // Class: IN
	response = append(response, 0x00, 0x00, 0x0e, 0x10) // TTL: 3600
	hostData := a.encodeName(a.hostname + "." + ServiceDomain)
	srvDataLen := 6 + len(hostData) // priority(2) + weight(2) + port(2) + hostname
	response = append(response, byte(srvDataLen>>8), byte(srvDataLen)) // Data length
	response = append(response, 0x00, 0x00) // Priority: 0
	response = append(response, 0x00, 0x00) // Weight: 0
	response = append(response, byte(a.port>>8), byte(a.port)) // Port
	response = append(response, hostData...)

	// TXT record with service info
	txtRecords := []string{
		fmt.Sprintf("version=%s", a.version),
		fmt.Sprintf("serverid=%s", a.serverID),
		"service=sentinel",
	}
	response = append(response, a.encodeName(serviceName)...)
	response = append(response, 0x00, 0x10) // Type: TXT
	response = append(response, 0x00, 0x01) // Class: IN
	response = append(response, 0x00, 0x00, 0x0e, 0x10) // TTL: 3600
	txtData := a.encodeTXT(txtRecords)
	response = append(response, byte(len(txtData)>>8), byte(len(txtData))) // Data length
	response = append(response, txtData...)

	// A record: hostname.local -> IP address
	response = append(response, a.encodeName(a.hostname+"."+ServiceDomain)...)
	response = append(response, 0x00, 0x01) // Type: A
	response = append(response, 0x00, 0x01) // Class: IN
	response = append(response, 0x00, 0x00, 0x0e, 0x10) // TTL: 3600
	response = append(response, 0x00, 0x04) // Data length: 4
	response = append(response, localIP.To4()...)

	return response
}

// encodeName encodes a DNS name
func (a *Advertiser) encodeName(name string) []byte {
	var result []byte
	parts := strings.Split(strings.TrimSuffix(name, "."), ".")
	for _, part := range parts {
		result = append(result, byte(len(part)))
		result = append(result, []byte(part)...)
	}
	result = append(result, 0x00) // End of name
	return result
}

// encodeTXT encodes TXT record data
func (a *Advertiser) encodeTXT(records []string) []byte {
	var result []byte
	for _, record := range records {
		result = append(result, byte(len(record)))
		result = append(result, []byte(record)...)
	}
	return result
}

// getHostname returns the local hostname
func getHostname() (string, error) {
	hostname, err := net.LookupAddr("127.0.0.1")
	if err == nil && len(hostname) > 0 {
		return strings.TrimSuffix(hostname[0], "."), nil
	}

	// Fallback to OS hostname
	return "sentinel-server", nil
}

// getLocalIP returns the local IP address
func getLocalIP() net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return net.ParseIP("127.0.0.1")
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP
			}
		}
	}

	return net.ParseIP("127.0.0.1")
}
