package jni

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 全局 Context 管理（用于点击停止时秒停）
var (
	proxyCtx    context.Context
	proxyCancel context.CancelFunc
	proxyMutex  sync.Mutex
)

func InitProxyContext() {
	proxyMutex.Lock()
	defer proxyMutex.Unlock()
	if proxyCancel != nil {
		proxyCancel()
	}
	proxyCtx, proxyCancel = context.WithCancel(context.Background())
}

func CancelProxyContext() {
	proxyMutex.Lock()
	defer proxyMutex.Unlock()
	if proxyCancel != nil {
		proxyCancel()
		proxyCancel = nil
	}
}

var (
	poolIPCache  map[string]bool
	poolIPMutex  sync.RWMutex
	lastLoadTime time.Time
)

type ResultItem struct {
	Address string `json:"address"`
}

func loadProxyPoolIPs() map[string]bool {
	poolIPMutex.Lock()
	defer poolIPMutex.Unlock()

	if time.Since(lastLoadTime) < 10*time.Second && poolIPCache != nil {
		return poolIPCache
	}

	cache := make(map[string]bool)
	paths := []string{
		filepath.Join("result", "result.json"),
		"result.json",
		"config_save.json",
	}

	var fileBytes []byte
	var err error
	for _, p := range paths {
		if fileBytes, err = os.ReadFile(p); err == nil {
			break
		}
	}

	if err == nil && len(fileBytes) > 0 {
		var items []ResultItem
		if json.Unmarshal(fileBytes, &items) == nil {
			for _, item := range items {
				if item.Address != "" {
					host, _, errSplit := net.SplitHostPort(item.Address)
					if errSplit == nil {
						cache[host] = true
					} else {
						cache[item.Address] = true
					}
				}
			}
		}
	}

	poolIPCache = cache
	lastLoadTime = time.Now()
	return cache
}

func isBypassedIP(targetIP, socksServer string) bool {
	if socksServer != "" {
		if host, _, err := net.SplitHostPort(socksServer); err == nil {
			if host == targetIP {
				return true
			}
		} else if socksServer == targetIP {
			return true
		}
	}
	pool := loadProxyPoolIPs()
	return pool[targetIP]
}

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
			packet := make([]byte, n)
			copy(packet, buf[:n])

			ForwardPacket(packet, tunAddr, gateway, socksServer, func(reply []byte) {
				_, _ = file.Write(reply)
			})
		}
	}
}

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
	case 6: // TCP
		dstIP := net.IP(packet[16:20]).String()
		if len(packet) < ihl+20 { // 确保包含完整的 TCP 头部（至少 20 字节）
			return
		}
		dstPort := int(packet[ihl+2])<<8 | int(packet[ihl+3])

		// 获取 TCP 头部长度和标志位
		tcpHeaderOffset := ihl
		dataOffsetAndReserved := packet[tcpHeaderOffset+12]
		tcpHeaderLen := int(dataOffsetAndReserved>>4) * 4
		if len(packet) < tcpHeaderOffset+14 {
			return
		}
		tcpFlags := packet[tcpHeaderOffset+13]

		// 计算应用层 Payload 长度
		// 总 IP 长度 - IP头长度 - TCP头长度
		totalIPLen := int(packet[2])<<8 | int(packet[3])
		payloadLen := totalIPLen - ihl - tcpHeaderLen

		isSYN := (tcpFlags & 0x02) != 0
		isFIN := (tcpFlags & 0x01) != 0
		isRST := (tcpFlags & 0x04) != 0

		// 核心通用过滤逻辑：
		// 1. 如果是纯控制包（SYN、FIN、RST）且没有实际应用层数据负载 (payloadLen <= 0)，
		//    绝对不转给 SOCKS5，直接静默丢弃，彻底根治所有 IP 的握手风暴与 EOF 报错！
		if (isSYN || isFIN || isRST) && payloadLen <= 0 {
			return
		}

		go forwardToSocks(socksServer, dstIP, dstPort, packet)

	case 17: // UDP
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

func handleICMPReply(packet []byte, ihl int, localGateway, tunAddr string, sendBack func([]byte)) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PureGo-Tunnel] Recovered from panic in handleICMPReply: %v", r)
		}
	}()

	srcIP := net.IP(packet[12:16]).String()
	dstIP := net.IP(packet[16:20]).String()

	if dstIP != localGateway && dstIP != tunAddr {
		return
	}

	reqType := packet[ihl]
	if reqType != 8 {
		return
	}

	reply := make([]byte, len(packet))
	copy(reply, packet)

	src := net.ParseIP(srcIP).To4()
	dst := net.ParseIP(dstIP).To4()
	if src == nil || dst == nil {
		return
	}
	copy(reply[12:16], dst)
	copy(reply[16:20], src)

	setIPChecksum(reply, ihl)

	reply[ihl] = 0
	reply[ihl+1] = 0
	setICMPChecksum(reply, ihl, len(reply))

	sendBack(reply)
}

func setIPChecksum(buf []byte, ihl int) {
	buf[10] = 0
	buf[11] = 0
	var sum uint32
	for i := 0; i < ihl; i += 2 {
		sum += uint32(buf[i])<<8 | uint32(buf[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(buf[10:], checksum)
}

func setICMPChecksum(buf []byte, headerStart, length int) {
	for i := headerStart; i < length; i += 2 {
		buf[i] = 0
		if i+1 < length {
			buf[i+1] = 0
		}
	}

	var sum uint32
	for i := headerStart; i < length; i += 2 {
		b1 := uint32(buf[i])
		var b2 uint32
		if i+1 < length {
			b2 = uint32(buf[i+1])
		}
		sum += (b1 << 8) | b2
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(buf[headerStart+2:], checksum)
}

func forwardToSocks(socksServer, targetIP string, targetPort int, packet []byte) {
	parsedIP := net.ParseIP(targetIP)
	if parsedIP == nil {
		return
	}

	// 1. 严格白名单与保留段过滤
	if !parsedIP.IsGlobalUnicast() || parsedIP.IsLoopback() || parsedIP.IsPrivate() || parsedIP.IsLinkLocalUnicast() || parsedIP.IsMulticast() {
		return
	}

	ip4 := parsedIP.To4()
	if ip4 != nil {
		if ip4[0] == 100 && (ip4[1]&192) == 64 {
			return
		}
		if ip4[0] == 198 && (ip4[1]&254) == 18 {
			return
		}
	}

	// 2. 检查是否命中代理服务器自身或代理地址池
	if isBypassedIP(targetIP, socksServer) {
		return
	}

	proxyMutex.Lock()
	currentCtx := proxyCtx
	proxyMutex.Unlock()

	if currentCtx == nil {
		currentCtx = context.Background()
	}

	ctx, cancel := context.WithTimeout(currentCtx, 5*time.Second)
	defer cancel()

	dialer := &net.Dialer{
		Timeout: 3 * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "tcp", socksServer)
	if err != nil {
		return
	}
	defer conn.Close()

	// SOCKS5 握手
	_, err = conn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return
	}

	resp := make([]byte, 2)
	_, err = io.ReadFull(conn, resp)
	if err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		return
	}

	req := []byte{0x05, 0x01, 0x00, 0x01}
	if ip4 == nil {
		return
	}
	req = append(req, ip4...)
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

	_, _ = conn.Write(packet)
}
