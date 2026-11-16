package jni

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"time"
)

// StartTun2Socks initiates the pure Go network proxy tunnel, forwarding IP packets via SOCKS5 proxy.
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

	go func() {
		defer file.Close()
		buf := make([]byte, 32768)
		for {
			n, err := file.Read(buf)
			if err != nil {
				log.Printf("[PureGo-Tunnel] TUN read error: %v", err)
				break
			}
			if n > 20 {
				packet := buf[:n]
				// Basic IPv4 header parsing
				versionAndIHL := packet[0]
				version := versionAndIHL >> 4
				if version != 4 {
					continue
				}
				ihl := int(versionAndIHL & 0x0F) * 4
				if n < ihl {
					continue
				}
				protocol := packet[9]
				// TCP (6) or UDP (17)
				if protocol == 6 || protocol == 17 {
					dstIP := net.IP(packet[16:20])
					if len(packet) < ihl + 4 {
						continue
					}
					// Destination port (Bytes 2-3 of transport header)
					dstPort := int(packet[ihl+2])<<8 | int(packet[ihl+3])

					// Forward TCP/UDP traffic to SOCKS5 server
					go forwardToSocks(socksServer, dstIP.String(), dstPort, packet)
				}
			}
		}
	}()
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

