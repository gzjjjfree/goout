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

var (
	poolIPCache  map[string]bool
	poolIPMutex  sync.RWMutex
	lastLoadTime time.Time
)

type ResultItem struct {
	Address string `json:"address"`
}

// loadProxyPoolIPs 动态加载代理地址池，增加日志和健壮性
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

	if err != nil {
		log.Printf("[PureGo-Tunnel] Notice: failed to load proxy pool config file: %v", err)
	} else if len(fileBytes) > 0 {
		var items []ResultItem
		if err := json.Unmarshal(fileBytes, &items); err == nil {
			for _, item := range items {
				if item.Address != "" {
					// 兼容可能带端口的地址
					host, _, errSplit := net.SplitHostPort(item.Address)
					if errSplit == nil {
						cache[host] = true
					} else {
						cache[item.Address] = true
					}
				}
			}
		} else {
			log.Printf("[PureGo-Tunnel] Warning: failed to unmarshal proxy pool json: %v", err)
		}
	}

	poolIPCache = cache
	lastLoadTime = time.Now()
	return cache
}

// isBypassedIP 检查目标 IP 是否属于代理池或需要强制排除的代理服务器自身地址
func isBypassedIP(targetIP, socksServer string) bool {
	// 1. 检查是否为代理服务器自身的 host
	if socksServer != "" {
		if host, _, err := net.SplitHostPort(socksServer); err == nil {
			if host == targetIP {
				return true
			}
		} else if socksServer == targetIP {
			return true
		}
	}

	// 2. 检查是否在 result.json 代理地址池中
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

// handleICMPReply 安全构造 ICMP 回复，并同时重算 IP 头与 ICMP 校验和
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
	if reqType != 8 { // 8 = ICMP Echo Request
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

	// 重算 IP 头部校验和
	setIPChecksum(reply, ihl)

	// 修改类型为 Echo Reply (0) 并重算 ICMP 校验和
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

// setICMPChecksum 安全计算 ICMP 校验和，避免奇数长度越界
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

	// 1. 严格白名单与保留段过滤：
	// 必须是全球单播地址 (IsGlobalUnicast)，且排除本地回环、私网、链路本地、CGNAT 及多播等
	if !parsedIP.IsGlobalUnicast() || parsedIP.IsLoopback() || parsedIP.IsPrivate() || parsedIP.IsLinkLocalUnicast() || parsedIP.IsMulticast() {
		return
	}

	// 排除运营商级 NAT (CGNAT: 100.64.0.0/10) 及其他特殊保留段
	ip4 := parsedIP.To4()
	if ip4 != nil {
		// 100.64.0.0/10
		if ip4[0] == 100 && (ip4[1]&192) == 64 {
			return
		}
		// 198.18.0.0/15 (Benchmarking)
		if ip4[0] == 198 && (ip4[1]&254) == 18 {
			return
		}
	}

	// 2. 检查是否命中代理服务器自身或代理地址池，彻底切断回环
	if isBypassedIP(targetIP, socksServer) {
		return
	}

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
