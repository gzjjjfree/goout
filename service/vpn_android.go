//go:build android

package service

import (
	"C"
	"fmt"
	"log"
	"sync"
	"unsafe"

	"fyne.io/fyne/v2/driver"

	"github.com/gzjjjfree/goout/jni"
)

/*
#cgo android LDFLAGS: -llog

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

// 注意：static 是关键。
// cgo 在 -buildmode=c-shared 模式下会把内联 C 代码的符号以全局链接属性发出，
// 若为普通全局函数，同一份符号可能被塞进两个 .o，链接时即 duplicate symbol。
// 改为 static 后每个文件各持一份局部符号，cgo 生成的 wrapper 在同一翻译单元内即可调用。

static void androidStartVpnService(uintptr_t envPtr, uintptr_t activityPtr, const char *socksAddr) {
    JNIEnv *env = (JNIEnv *)envPtr;
    jobject activity = (jobject)activityPtr;
    if (!env || !activity) {
        __android_log_print(ANDROID_LOG_ERROR, "GoOutVpn", "androidStartVpnService: invalid env/activity");
        return;
    }
    jclass cls = (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    if (!cls) {
        __android_log_print(ANDROID_LOG_ERROR, "GoOutVpn", "FindClass GoOutVpnService failed");
        return;
    }
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "startVpnService",
                                              "(Landroid/content/Context;Ljava/lang/String;)V");
    if (!mid) {
        __android_log_print(ANDROID_LOG_ERROR, "GoOutVpn", "GetStaticMethodID startVpnService failed");
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, cls);
        return;
    }
    const char *addr = socksAddr ? socksAddr : "127.0.0.1:10909";
    jstring jaddr = (*env)->NewStringUTF(env, addr);
    (*env)->CallStaticVoidMethod(env, cls, mid, activity, jaddr);
    if ((*env)->ExceptionCheck(env)) {
        __android_log_print(ANDROID_LOG_ERROR, "GoOutVpn", "exception thrown by Java startVpnService");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    (*env)->DeleteLocalRef(env, jaddr);
    (*env)->DeleteLocalRef(env, cls);
}

static void androidStopVpnService(uintptr_t envPtr, uintptr_t activityPtr) {
    JNIEnv *env = (JNIEnv *)envPtr;
    jobject activity = (jobject)activityPtr;
    if (!env || !activity) {
        return;
    }
    jclass cls = (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    if (!cls) {
        return;
    }
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "stopVpnService",
                                              "(Landroid/content/Context;)V");
    if (!mid) {
        (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, cls);
        return;
    }
    (*env)->CallStaticVoidMethod(env, cls, mid, activity);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    (*env)->DeleteLocalRef(env, cls);
}
*/
import "C"

var (
	androidMu        sync.Mutex
	androidFD        int = -1
	protectFunc      func(int) bool
	activeVPN        *VPNServiceManager
	currentSocksAddr string
)

// triggerAndroidVpnService 通过 JNI 让 Android 宿主启动 GoOutVpnService。
// 注意：必须在 driver.RunNative 回调内部调用 native 函数，此时 env 才有效。
func triggerAndroidVpnService(socksAddr string) error {
	return driver.RunNative(func(ctx any) error {
		ac, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return fmt.Errorf("not a driver.AndroidContext")
		}
		cAddr := C.CString(socksAddr)
		defer C.free(unsafe.Pointer(cAddr))
		C.androidStartVpnService(C.uintptr_t(uintptr(ac.Env)), C.uintptr_t(uintptr(ac.Ctx)), cAddr)
		return nil
	})
}

// triggerAndroidStopVpnService 通过 JNI 让 Android 宿主停止 GoOutVpnService。
func triggerAndroidStopVpnService() {
	_ = driver.RunNative(func(ctx any) error {
		ac, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return nil
		}
		C.androidStopVpnService(C.uintptr_t(uintptr(ac.Env)), C.uintptr_t(uintptr(ac.Ctx)))
		return nil
	})
}

// StartVPN 触发生 Android VpnService 建立 TUN 接口。
// 真实的 fd 会通过 JNI (nativeStartVPNWithFD -> updateAndroidFD) 回传，
// 到时再在该回调里用真实 fd 启动 tun2socks。
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	// 记录期望的 socks 地址与本 manager，JNI 回调即可把真实 TUN fd 交给我们。
	androidMu.Lock()
	currentSocksAddr = socksAddr
	activeVPN = v
	androidFD = -1
	androidMu.Unlock()

	log.Printf("[Android-VPN] Starting VPN, socks=%s (waiting for TUN FD from host)", socksAddr)

	if err := triggerAndroidVpnService(socksAddr); err != nil {
		androidMu.Lock()
		if activeVPN == v {
			activeVPN = nil
		}
		androidMu.Unlock()
		return fmt.Errorf("start android vpn: %w", err)
	}

	// 乐观地标记为 active，让 UI 显示“连接中”。
	// tun2socks 引擎会在 updateAndroidFD 收到真实 fd 后再(重)启动。
	v.active = true
	return nil
}

// updateAndroidFD 由 JNI 桥接调用，接收 Android VpnService 提供的真实 TUN fd，
// 用该 fd 启动(或重启)tun2socks 引擎，从而接管全部流量。
func updateAndroidFD(fd int) {
	androidMu.Lock()
	androidFD = fd
	v := activeVPN
	socks := currentSocksAddr
	androidMu.Unlock()

	if v == nil {
		log.Printf("[Android-VPN] updateAndroidFD(fd=%d): no active VPN manager, ignoring", fd)
		return
	}

	socksToUse := socks
	if socksToUse == "" {
		socksToUse = "127.0.0.1:10908"
	}

	v.mu.Lock()
	v.active = true
	v.mu.Unlock()

	log.Printf("[Android-VPN] Real TUN FD=%d received, starting tun2socks (socks=%s)", fd, socksToUse)

	// 用真实 fd 重建 engine。
	// 关键：放到后台 goroutine，避免阻塞 service/UI 主线程
	go func() {
		jni.StopTun2Socks()
		if err := jni.StartTun2Socks(fd, v.tunIP, v.gateway, socksToUse); err != nil {
			log.Printf("[Android-VPN] Failed to start tun2socks with real FD=%d: %v", fd, err)
			v.mu.Lock()
			v.active = false
			v.mu.Unlock()
			return
		}
		log.Printf("[Android-VPN] tun2socks running on real FD=%d", fd)
	}()
}

// StopVPN 停止 tun2socks，并让 Android 宿主回收 TUN 接口。
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Android-VPN] Stopping Android VpnService TUN tunnel")

	// 先停 tun2socks，再让宿主关闭 TUN 接口，避免仍在写 fd。
	jni.StopTun2Socks()
	triggerAndroidStopVpnService()

	androidMu.Lock()
	androidFD = -1
	if activeVPN == v {
		activeVPN = nil
	}
	androidMu.Unlock()

	v.active = false
	return nil
}

// StartVPNWithFD 保留历史导出名以保持 ABI 兼容。
//
//export StartVPNWithFD
func StartVPNWithFD(fd int, socksAddr string) {
	log.Printf("[Android-VPN] Received VpnService FD=%d (socks: %s)", fd, socksAddr)
	if socksAddr != "" {
		androidMu.Lock()
		currentSocksAddr = socksAddr
		androidMu.Unlock()
	}
	updateAndroidFD(fd)
}

// JNI 桥接: GoOutVpnService.java 的 nativeStartVPNWithFD -> updateAndroidFD
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD(env uintptr, thiz uintptr, fd int, socksAddrPtr uintptr) {
	log.Printf("[Android-JNI] nativeStartVPNWithFD called from Android VpnService with FD=%d", fd)
	updateAndroidFD(fd)
}

// RegisterProtectCallback registers a callback function from Java/Kotlin to protect socket fds via VpnService.protect(fd).
func RegisterProtectCallback(fn func(int) bool) {
	protectFunc = fn
}

// ProtectSocket calls the registered protect function to exempt a socket FD from VPN routing.
//
//export ProtectSocket
func ProtectSocket(fd int) bool {
	if protectFunc != nil {
		success := protectFunc(fd)
		log.Printf("[Android-VPN] ProtectSocket fd=%d, success=%v", fd, success)
		return success
	}
	log.Printf("[Android-VPN] ProtectSocket fd=%d, no protect callback registered", fd)
	return false
}

// JNI export for Java-side socket protection registration if needed
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeRegisterProtect
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeRegisterProtect(env uintptr, thiz uintptr) {
	log.Println("[Android-JNI] nativeRegisterProtect called")
}
