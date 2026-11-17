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

// 缓存代理地址池的 IP 集合，避免每次发包都去读文件
var (
	poolIPCache  map[string]bool
	poolIPMutex  sync.RWMutex
	lastLoadTime time.Time
)

// ResultItem 对应 /result/result.json 中的节点结构
type ResultItem struct {
	Address string `json:"address"`
}

// loadProxyPoolIPs 动态加载 /result/result.json 中的 IP 地址池
func loadProxyPoolIPs() map[string]bool {
	poolIPMutex.Lock()
	defer poolIPMutex.Unlock()

	// 缓存 10 秒，避免频繁读取磁盘文件
	if time.Since(lastLoadTime) < 10*time.Second && poolIPCache != nil {
		return poolIPCache
	}

	cache := make(map[string]bool)

	// 尝试寻找 result.json 文件的可能路径
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
		if err := json.Unmarshal(fileBytes, &items); err == nil {
			for _, item := range items {
				if item.Address != "" {
					cache[item.Address] = true
				}
			}
		} else {
			// 兼容可能不是标准数组的格式，尝试解析通用 map
			var rawMap map[string]interface{}
			if errMap := json.Unmarshal(fileBytes, &rawMap); errMap == nil {
				// 如果有其他结构可以在此适配
			}
		}
	}

	poolIPCache = cache
	lastLoadTime = time.Now()
	return cache
}

// isPoolIP 检查目标 IP 是否属于代理地址池
func isPoolIP(targetIP string) bool {
	pool := loadProxyPoolIPs()
	return pool[targetIP]
}

// StartTun2Socks 初始化纯 Go 网络代理隧道
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

// RunTunEngine 驱动基于真实 TUN 文件描述符的数据包转发引擎
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

// ForwardPacket 处理收到的原始 IP 数据包
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

// handleICMPReply 构造针对本地网关/TUN IP 的 ICMP Echo Reply
func handleICMPReply(packet []byte, ihl int, localGateway, tunAddr string, sendBack func([]byte)) {
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

	reply[ihl] = 0
	reply[ihl+1] = 0
	setICMPChecksum(reply, ihl, len(reply))

	sendBack(reply)
}

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

// forwardToSocks 连接 SOCKS5 代理并转发流量，同时加入地址池与本地回环过滤防死循环
func forwardToSocks(socksServer, targetIP string, targetPort int, packet []byte) {
	// ==================== 核心防回环/防无限直连校验 ====================

	// 1. 过滤本地回环、网关及私有地址
	if targetIP == "127.0.0.1" || targetIP == "0.0.0.0" || targetIP == "10.0.0.1" {
		return
	}

	// 2. 核心：如果目标 IP 存在于你的 /result/result.json 地址池中，
	// 说明这是代理节点自身的出口流量，绝对不能再次代理由 SOCKS5 发送，直接拦截丢弃或放行直连！
	if isPoolIP(targetIP) {
		// 属于地址池节点，直接返回不处理，切断死循环套娃
		return
	}

	// ==============================================================

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
