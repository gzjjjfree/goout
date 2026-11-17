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

// StartTun2Socks 初始化纯 Go 网络代理隧道，处理 IP 数据包并通过 SOCKS5 代理转发。
// fd 为 TUN 接口的文件描述符（0 或负数表示无真实设备的模拟模式）。
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

	go RunTunEngine(file, tunAddr, gateway, socksServer)
}

// RunTunEngine 驱动基于真实 TUN 文件描述符的数据包转发引擎。
// 它持续读取原始 IP 数据包，并对发往本地网关的 ICMP 请求进行回复（使 `ping <tunIP>` 成功）。
func RunTunEngine(file *os.File, tunAddr, gateway, socksServer string) {
	defer file.Close()

	buf := make([]byte, 32768)
	for {
		n, err := file.Read(buf)
		if err != nil {
			log.Printf("[PureGo-Tunnel] TUN read error: %v", err)
			return
		}
		if n > 20 {
			// 创建当前包的独立切片，防止并发读写污染缓冲区
			packet := make([]byte, n)
			copy(packet, buf[:n])

			ForwardPacket(packet, tunAddr, gateway, socksServer, func(reply []byte) {
				_, _ = file.Write(reply)
			})
		}
	}
}

// ForwardPacket 处理单个接收到的原始 IP 数据包：
// 识别并回复网关的 ICMP 请求，同时将 TCP/UDP 流量导向 SOCKS5 代理。
func ForwardPacket(packet []byte, tunAddr, gateway, socksServer string, sendBack func([]byte)) {
	if len(packet) < 20 {
		return
	}

	versionAndIHL := packet[0]
	if versionAndIHL>>4 != 4 {
		return // 仅处理 IPv4
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
	case 6, 17: // TCP 或 UDP
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

// handleICMPReply 构造针对本地网关/TUN IP 的 ICMP Echo Reply，使 `ping` 成功。
func handleICMPReply(packet []byte, ihl int, localGateway, tunAddr string, sendBack func([]byte)) {
	srcIP := net.IP(packet[12:16]).String()
	dstIP := net.IP(packet[16:20]).String()

	if dstIP != localGateway && dstIP != tunAddr {
		return
	}

	reqType := packet[ihl]
	if reqType != 8 { // 8 = ICMP Echo Request (请求)
		return
	}

	reply := make([]byte, len(packet))
	copy(reply, packet)

	// 交换源与目的 IP
	src := net.ParseIP(srcIP).To4()
	dst := net.ParseIP(dstIP).To4()
	if src == nil || dst == nil {
		return
	}
	copy(reply[12:16], dst)
	copy(reply[16:20], src)

	// 修改类型为 Echo Reply (0) 并重新计算校验和
	reply[ihl] = 0
	reply[ihl+1] = 0
	setICMPChecksum(reply, ihl, len(reply))

	sendBack(reply)
}

// setICMPChecksum 重新计算 ICMP 校验和
func setICMPChecksum(buf []byte, headerStart, length int) {
	for i := headerStart; i < length; i += 2 {
		buf[i] = 0
		buf[i+1] = 0
	}

	var sum uint32
	for i := headerStart; i < length; i += 2 {
		sum += uint32(buf[i])<<8 | uint32(buf[i+1])
	}
	if (length-headerStart)&1 == 1 {
		sum += uint32(buf[length-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(buf[headerStart+2:], checksum)
}

// forwardToSocks 连接 SOCKS5 代理并转发流量
func forwardToSocks(socksServer, targetIP string, targetPort int, packet []byte) {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := dialer.DialContext(ctx, "tcp", socksServer)
	if err != nil {
		return
	}
	defer conn.Close()

	// SOCKS5 握手 (无认证: 0x00)
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return
	}

	resp := make([]byte, 2)
	_, err = io.ReadFull(conn, resp)
	if err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		return
	}

	// SOCKS5 连接请求
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

	// 转发净荷数据
	_, _ = conn.Write(packet)
}
