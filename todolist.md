# GoOut 项目待办事项清单 (TODO List)

## 1. 核心骨架与基础服务
- [x] 初始化 Fyne 界面框架入口 (`main.go`)
- [x] 实现进程管理器核心逻辑 (`core/process.go`)，支持 Shizuku/非 root 二进制生命周期管理与线程安全日志环形缓存
- [ ] 完善配置存储与管理模块 (`data/config.go`)

## 2. VPN 与流量重定向模块
- [ ] 完善 Android `VpnService` 绑定与 TUN 虚拟网卡生命周期管理 (`service/vpn.go`)
- [ ] 实现 `tun2socks` CGO/JNI 动态库绑定与数据包转发 (`jni/tun2socks.go`)

## 3. UI 交互与状态绑定
- [ ] 将控制面板按钮与 `ProcessManager` 及 `VPNServiceManager` 进行绑定 (`ui/dashboard.go`)
- [ ] 实现实时日志流在 Fyne UI 终端上的异步安全更新（结合 `fyne.Do`）
- [ ] 增加配置项编辑页面（代理服务器地址、端口、订阅管理等）

## 4. 构建与测试
- [ ] 配置 `FyneApp.toml` 跨平台打包与 `fyne-cross` 编译脚本
- [ ] 编写核心模块单元测试
