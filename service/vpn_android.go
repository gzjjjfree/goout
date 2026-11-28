package service

import (
	"C"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"fyne.io/fyne/v2/driver"

	"github.com/gzjjjfree/goout/jni"
)

/*
#cgo android LDFLAGS: -llog

#include <jni.h>
#include <android/log.h>
#include <stdlib.h>

#define GOOUT_LOG_TAG "GoOutVpnService"

static jclass goout_findVpnServiceClass(JNIEnv *env, jobject activity) {
    if (!activity) {
        __android_log_print(ANDROID_LOG_WARN, GOOUT_LOG_TAG, "goout_findVpnServiceClass: activity is null, using FindClass");
        return (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    }
    jclass activityClass = (*env)->GetObjectClass(env, activity);
    if (!activityClass) {
        __android_log_print(ANDROID_LOG_WARN, GOOUT_LOG_TAG, "GetObjectClass failed, fallback to FindClass");
        return (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    }
    jmethodID getClassLoader = (*env)->GetMethodID(env, activityClass, "getClassLoader", "()Ljava/lang/ClassLoader;");
    (*env)->DeleteLocalRef(env, activityClass);
    if (!getClassLoader) {
        return (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    }
    jobject classLoader = (*env)->CallObjectMethod(env, activity, getClassLoader);
    if (!classLoader) {
        return (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    }
    jclass classLoaderClass = (*env)->FindClass(env, "java/lang/ClassLoader");
    if (!classLoaderClass) {
        (*env)->DeleteLocalRef(env, classLoader);
        return (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    }
    jmethodID loadClass = (*env)->GetMethodID(env, classLoaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
    (*env)->DeleteLocalRef(env, classLoaderClass);
    if (!loadClass) {
        (*env)->DeleteLocalRef(env, classLoader);
        return (*env)->FindClass(env, "com/gzjjjfree/goout/GoOutVpnService");
    }
    jstring className = (*env)->NewStringUTF(env, "com.gzjjjfree.goout.GoOutVpnService");
    jclass cls = (jclass)(*env)->CallObjectMethod(env, classLoader, loadClass, className);
    (*env)->DeleteLocalRef(env, className);
    (*env)->DeleteLocalRef(env, classLoader);
    if ((*env)->ExceptionCheck(env)) {
        __android_log_print(ANDROID_LOG_ERROR, GOOUT_LOG_TAG, "Exception while loading GoOutVpnService via ClassLoader");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
        return NULL;
    }
    return cls;
}

static void androidStartVpnService(uintptr_t envPtr, uintptr_t activityPtr, const char *socksAddr) {
    JNIEnv *env = (JNIEnv *)envPtr;
    jobject activity = (jobject)activityPtr;
    if (!env || !activity) {
        __android_log_print(ANDROID_LOG_ERROR, GOOUT_LOG_TAG, "androidStartVpnService: invalid env=%p or activity=%p", env, activity);
        return;
    }
    __android_log_print(ANDROID_LOG_INFO, GOOUT_LOG_TAG, "androidStartVpnService: env=%p, activity=%p, socks=%s",
                        env, activity, socksAddr ? socksAddr : "");
    jclass cls = goout_findVpnServiceClass(env, activity);
    if (!cls) {
        __android_log_print(ANDROID_LOG_ERROR, GOOUT_LOG_TAG, "FindClass/LoadClass GoOutVpnService failed");
        return;
    }
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "startVpnService",
                                              "(Landroid/content/Context;Ljava/lang/String;)V");
    if (!mid) {
        __android_log_print(ANDROID_LOG_ERROR, GOOUT_LOG_TAG, "GetStaticMethodID startVpnService failed");
        if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionDescribe(env);
            (*env)->ExceptionClear(env);
        }
        (*env)->DeleteLocalRef(env, cls);
        return;
    }
    const char *addr = socksAddr ? socksAddr : "127.0.0.1:10909";
    jstring jaddr = (*env)->NewStringUTF(env, addr);
    (*env)->CallStaticVoidMethod(env, cls, mid, activity, jaddr);
    if ((*env)->ExceptionCheck(env)) {
        __android_log_print(ANDROID_LOG_ERROR, GOOUT_LOG_TAG, "exception thrown by Java startVpnService");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    } else {
        __android_log_print(ANDROID_LOG_INFO, GOOUT_LOG_TAG, "Java startVpnService returned normally");
    }
    (*env)->DeleteLocalRef(env, jaddr);
    (*env)->DeleteLocalRef(env, cls);
}

static void androidStopVpnService(uintptr_t envPtr, uintptr_t activityPtr) {
    JNIEnv *env = (JNIEnv *)envPtr;
    jobject activity = (jobject)activityPtr;
    if (!env || !activity) {
        __android_log_print(ANDROID_LOG_WARN, GOOUT_LOG_TAG, "androidStopVpnService: invalid env or activity");
        return;
    }
    jclass cls = goout_findVpnServiceClass(env, activity);
    if (!cls) {
        return;
    }
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "stopVpnService",
                                              "(Landroid/content/Context;)V");
    if (!mid) {
        if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
        }
        (*env)->DeleteLocalRef(env, cls);
        return;
    }
    (*env)->CallStaticVoidMethod(env, cls, mid, activity);
    if ((*env)->ExceptionCheck(env)) {
        __android_log_print(ANDROID_LOG_ERROR, GOOUT_LOG_TAG, "exception thrown by Java stopVpnService");
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    (*env)->DeleteLocalRef(env, cls);
}

static jboolean androidProtectSocket(uintptr_t envPtr, uintptr_t activityPtr, jint fd) {
    JNIEnv *env = (JNIEnv *)envPtr;
    jobject activity = (jobject)activityPtr;
    if (!env || !activity) {
        return JNI_FALSE;
    }
    jclass cls = goout_findVpnServiceClass(env, activity);
    if (!cls) {
        return JNI_FALSE;
    }
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "protectFd", "(I)Z");
    if (!mid) {
        if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
        }
        (*env)->DeleteLocalRef(env, cls);
        return JNI_FALSE;
    }
    jboolean res = (*env)->CallStaticBooleanMethod(env, cls, mid, fd);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
    (*env)->DeleteLocalRef(env, cls);
    return res;
}
*/
import "C"

var (
	androidMu        sync.Mutex
	androidFD        int = -1
	protectFunc      func(int) bool
	activeVPN        *VPNServiceManager
	currentSocksAddr string
	fdReadyCh        chan int
)

func init() {
	RegisterProtectCallback(func(fd int) bool {
		if fd < 0 {
			return false
		}
		var success bool
		_ = driver.RunNative(func(ctx any) error {
			ac, ok := ctx.(*driver.AndroidContext)
			if !ok || ac.Env == 0 || ac.Ctx == 0 {
				return fmt.Errorf("invalid android context")
			}
			res := C.androidProtectSocket(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), C.jint(fd))
			success = res != 0
			return nil
		})
		return success
	})

	protectSocketFD = func(fd int) bool {
		return ProtectSocket(fd)
	}
}

func resetFDReadyChannel() {
	androidMu.Lock()
	fdReadyCh = make(chan int, 1)
	androidFD = -1
	androidMu.Unlock()
}

// emitLog 通过 activeVPN 的 onLog 回调把消息推入 UI 日志栏（若回调存在）。
// 同时保留 Go 标准 log 输出，确保 goout.log 文件仍记录完整日志。
func emitLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s", msg)
	androidMu.Lock()
	v := activeVPN
	androidMu.Unlock()
	if v != nil && v.onLog != nil {
		v.onLog(msg)
	}
}

// WaitForAndroidFD 等待系统授权并建立 TUN 接口后传回真实 FD（超时返回 false）。
func WaitForAndroidFD(timeout time.Duration) (int, bool) {
	androidMu.Lock()
	ch := fdReadyCh
	existingFD := androidFD
	androidMu.Unlock()

	if existingFD >= 0 {
		return existingFD, true
	}
	if ch == nil {
		return -1, false
	}

	select {
	case fd := <-ch:
		return fd, fd >= 0
	case <-time.After(timeout):
		return -1, false
	}
}

// triggerAndroidVpnService 通过 JNI 让 Android 宿主启动 GoOutVpnService。
func triggerAndroidVpnService(socksAddr string) error {
	emitLog("[Android-VPN] Calling driver.RunNative to trigger GoOutVpnService, socks=%s", socksAddr)
	err := driver.RunNative(func(ctx any) error {
		ac, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return fmt.Errorf("not a driver.AndroidContext (got %T)", ctx)
		}
		if ac.Env == 0 || ac.Ctx == 0 {
			return fmt.Errorf("driver.AndroidContext has zero Env or Ctx")
		}
		cAddr := C.CString(socksAddr)
		defer C.free(unsafe.Pointer(cAddr))
		C.androidStartVpnService(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), cAddr)
		return nil
	})
	if err != nil {
		emitLog("[Android-VPN] driver.RunNative failed: %v", err)
	}
	return err
}

// triggerAndroidStopVpnService 通过 JNI 让 Android 宿主停止 GoOutVpnService。
func triggerAndroidStopVpnService() {
	err := driver.RunNative(func(ctx any) error {
		ac, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return nil
		}
		if ac.Env == 0 || ac.Ctx == 0 {
			return nil
		}
		C.androidStopVpnService(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx))
		return nil
	})
	if err != nil {
		emitLog("[Android-VPN] triggerAndroidStopVpnService error: %v", err)
	}
}

// StartVPN 触发 Android VpnService 建立 TUN 接口，并等待真实 TUN FD。
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	if v.active {
		v.mu.Unlock()
		return nil
	}
	v.mu.Unlock()

	resetFDReadyChannel()

	androidMu.Lock()
	currentSocksAddr = socksAddr
	activeVPN = v
	androidMu.Unlock()

	v.emitLog("[Android-VPN] Requesting Android VPN permission/service, socks=%s", socksAddr)

	if err := triggerAndroidVpnService(socksAddr); err != nil {
		androidMu.Lock()
		if activeVPN == v {
			activeVPN = nil
		}
		androidMu.Unlock()
		return fmt.Errorf("start android vpn: %w", err)
	}

	fd, ok := WaitForAndroidFD(8 * time.Second)
	if !ok || fd < 0 {
		androidMu.Lock()
		if activeVPN == v {
			activeVPN = nil
		}
		androidMu.Unlock()
		v.mu.Lock()
		v.active = false
		v.mu.Unlock()
		return fmt.Errorf("未收到系统 VPN TUN FD（可能授权弹窗未确认/被系统关闭，或已有其他 VPN 占用）")
	}

	v.mu.Lock()
	v.active = true
	v.mu.Unlock()
	return nil
}

// updateAndroidFD 由 JNI 桥接调用，接收 Android VpnService 提供的真实 TUN fd，
// 用该 fd 启动(或重启)tun2socks 引擎，从而接管全部流量。
func updateAndroidFD(fd int) {
	if fd < 0 {
		emitLog("[Android-VPN] updateAndroidFD received invalid fd=%d", fd)
		return
	}

	androidMu.Lock()
	androidFD = fd
	v := activeVPN
	socks := currentSocksAddr
	ch := fdReadyCh
	androidMu.Unlock()

	if ch != nil {
		select {
		case ch <- fd:
		default:
		}
	}

	if v == nil {
		emitLog("[Android-VPN] updateAndroidFD(fd=%d): no active VPN manager, ignoring", fd)
		return
	}

	socksToUse := socks
	if socksToUse == "" {
		socksToUse = "127.0.0.1:10908"
	}

	emitLog("[Android-VPN] Real TUN FD=%d received, starting tun2socks (socks=%s)", fd, socksToUse)

	go func() {
		jni.StopTun2Socks()
		if err := jni.StartTun2Socks(fd, v.tunIP, v.gateway, socksToUse); err != nil {
			emitLog("[Android-VPN] Failed to start tun2socks with real FD=%d: %v", fd, err)
			v.mu.Lock()
			v.active = false
			v.mu.Unlock()
			return
		}
		v.mu.Lock()
		v.active = true
		v.mu.Unlock()
		emitLog("[Android-VPN] tun2socks running on real FD=%d", fd)
	}()
}

// StopVPN 停止 tun2socks，并让 Android 宿主回收 TUN 接口。
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active && activeVPN != v {
		return nil
	}

	v.emitLog("[Android-VPN] Stopping Android VpnService TUN tunnel")

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
	emitLog("[Android-VPN] Received VpnService FD=%d (socks: %s)", fd, socksAddr)
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
	emitLog("[Android-JNI] nativeStartVPNWithFD called from Android VpnService with FD=%d", fd)
	updateAndroidFD(fd)
}

// JNI 桥接: 用户拒绝或取消 VPN 授权，或建立接口失败
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeOnVpnPermissionDenied
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeOnVpnPermissionDenied(env uintptr, thiz uintptr) {
	emitLog("[Android-JNI] nativeOnVpnPermissionDenied called")
	androidMu.Lock()
	v := activeVPN
	activeVPN = nil
	androidFD = -1
	ch := fdReadyCh
	androidMu.Unlock()

	if ch != nil {
		select {
		case ch <- -1:
		default:
		}
	}

	if v != nil {
		v.mu.Lock()
		v.active = false
		v.mu.Unlock()
	}
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
		// emitLog("[Android-VPN] ProtectSocket fd=%d, success=%v", fd, success)
		return success
	}
	emitLog("[Android-VPN] ProtectSocket fd=%d, no protect callback registered", fd)
	return false
}

// JNI export for Java-side socket protection registration if needed
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeRegisterProtect
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeRegisterProtect(env uintptr, thiz uintptr) {
	emitLog("[Android-JNI] nativeRegisterProtect called")
}
