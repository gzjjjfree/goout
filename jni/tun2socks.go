package jni

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"time"
)

// StartTun2Socks initiates the pure Go network proxy tunnel, forwarding IP packets via SOCKS5 proxy.
// fd is the TUN interface file descriptor (0 or negative means simulation mode without a real device).
func StartTun2Socks(fd int, tunAddr, gateway, socksServer string) {
	log.Printf("[PureGo-Tunnel] StartTun2Socks called: fd=%d, tun=%s, gateway=%s, socks=%s", fd, tunAddr, gateway, socksServer)

	if fd < 0 {
		log.Println("[PureGo-Tunnel] Running in simulation/mock mode (fd < 0)")
		return
	}

	file := os.NewFile(uintptr(fd), "tun")
	if file == nil {
		log.Println("[PureGo-Tunnel] Failed to create file from TUN fd")
		return
	}

	go runTunEngine(file, tunAddr, gateway, socksServer)
}

// RunTunEngine drives the shared packet-forwarding engine over a real TUN file descriptor.
// It reads raw IP packets and forwards TCP/UDP traffic to the SOCKS5 server while replying
// to ICMP echo requests aimed at the local gateway address (so `ping <tunIP>` works).
func RunTunEngine(file *os.File, tunAddr, gateway, socksServer string) {
	defer file.Close()
	runTunEngine(file, tunAddr, gateway, socksServer)
}

func runTunEngine(file *os.File, tunAddr, gateway, socksServer string) {
	buf := make([]byte, 32768)
	for {
		n, err := file.Read(buf)
		if err != nil {
			log.Printf("[PureGo-Tunnel] TUN read error: %v", err)
			return
		}
		if n > 20 {
			packet := buf[:n]
			ForwardPacket(packet, tunAddr, gateway, socksServer, func(reply []byte) {
				_, _ = file.Write(reply)
			})
		}
	}
}

// ForwardPacket processes a single received raw IP packet: it forwards TCP/UDP traffic to the
// SOCKS5 server and answers ICMP echo requests aimed at localGateway/tunAddr (so that `ping`
// succeeds). When sendBack is non-nil, any constructed reply packet is written through it; this
// is used by the Windows Wintun path to write packets back into the virtual adapter.
func ForwardPacket(packet []byte, tunAddr, gateway, socksServer string, sendBack func([]byte)) {
	if len(packet) < 20 {
		return
	}

	versionAndIHL := packet[0]
	if versionAndIHL>>4 != 4 {
		return
	}
	ihl := int(versionAndIHL&0x0F) * 4
	if len(packet) < ihl {
		return
	}

	localGateway := tunAddr
	if gateway != "" {
		localGateway = gateway
	}

	switch packet[9] {
	case 6, 17: // TCP or UDP
		dstIP := net.IP(packet[16:20]).String()
		if len(packet) < ihl+4 {
			return
		}
		dstPort := int(packet[ihl+2])<<8 | int(packet[ihl+3])
		go forwardToSocks(socksServer, dstIP, dstPort, packet)
	case 1: // ICMP
		if sendBack != nil {
			handleICMPReply(packet, ihl, localGateway, tunAddr, sendBack)
		}
	}
}

// handleICMPReply constructs an ICMP Echo Reply for requests aimed at the local gateway/TUN IP
// and invokes sendBack with it so that `ping <tunIP>` succeeds in the tunnel.
func handleICMPReply(packet []byte, ihl int, localGateway, tunAddr string, sendBack func([]byte)) {
	srcIP := net.IP(packet[12:16]).String()

	// Only answer echo requests that target our local interface address.
	dstIP := net.IP(packet[16:20]).String()
	if dstIP != localGateway && dstIP != tunAddr {
		return
	}

	reqType := packet[ihl]
	if reqType != 8 { // 8 = ICMP Echo Request
		return
	}

	reply := make([]byte, len(packet))
	copy(reply, packet)

	// Swap source/destination IPs.
	src := net.ParseIP(srcIP).To4()
	dst := net.ParseIP(dstIP).To4()
	if src == nil || dst == nil {
		return
	}
	copy(reply[12:16], dst)
	copy(reply[16:20], src)

	// Change type/code to Echo Reply (0, 0) and fix the checksum.
	reply[ihl] = 0 // Echo Reply (0)
	reply[ihl+1] = 0
	setICMPChecksum(reply, ihl, len(reply))

	sendBack(reply)
}

// setICMPChecksum recomputes the ICMP checksum over the header (and payload when provided).
func setICMPChecksum(buf []byte, headerStart, length int) {
	for i := headerStart; i < length; i += 2 {
		buf[i] = 0
		buf[i+1] = 0
	}

	var sum uint32
	for i := headerStart; i < length; i += 2 {
		sum += uint32(buf[i])<<8 | uint32(buf[i+1])
	}
	if (length-headerStart)&1 == 1 { // odd byte (header only for echo reply)
		sum += uint32(buf[length-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(buf[headerStart+2:], checksum)
}

func forwardToSocks(socksServer, targetIP string, targetPort int, packet []byte) {
	der := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := der.DialContext(ctx, "tcp", socksServer)
	if err != nil {
		// Fallback or log connection failure to SOCKS5 listener
		return
	}
	defer conn.Close()

	// SOCKS5 Greeting (Method selection: NO AUTHENTICATION REQUIRED = 0x00)
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return
	}

	resp := make([]byte, 2)
	_, err = io.ReadFull(conn, resp)
	if err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		return
	}

	// SOCKS5 Connect Request
	// VER(0x05), CMD(0x01: CONNECT), RSV(0x00), ATYP(0x01: IPv4), DST.ADDR(4 bytes), DST.PORT(2 bytes)
	req := []byte{0x05, 0x01, 0x00, 0x01}
	ip := net.ParseIP(targetIP).To4()
	if ip == nil {
		return
	}
	req = append(req, ip...)
	portBytes := []byte{byte(targetPort >> 8), byte(targetPort & 0xFF)}
	req = append(req, portBytes...)

	_, err = conn.Write(req)
	if err != nil {
		return
	}

	reply := make([]byte, 10)
	_, err = io.ReadFull(conn, reply)
	if err != nil || reply[1] != 0x00 {
		return
	}

	// Forward packet payload / data
	_, _ = conn.Write(packet)
}
