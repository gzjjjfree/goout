package com.gzjjjfree.goout;

import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.util.Log;
import java.io.IOException;

/**
 * Android VpnService 宿主：负责调起系统 VPN、建立 TUN 接口，
 * 并通过 JNI 把真实 fd 传回 Go 侧，由 tun2socks 接管流量。
 */
public class GoOutVpnService extends VpnService {
    private static final String TAG = "GoOutVpnService";
    private static final int REQUEST_CODE_PREPARE = 0x2001;

    private ParcelFileDescriptor mInterface;
    private static GoOutVpnService sInstance;
    private static String currentSocksAddr = "127.0.0.1:10808";

    static {
        // 关键：加载 Go 编译出来的原生库，否则 nativeStartVPNWithFD 会 UnsatisfiedLinkError
        System.loadLibrary("GoOut");
    }

    public GoOutVpnService() {
        // 构造时再次确保本地库已加载
        System.loadLibrary("GoOut");
    }

    public static GoOutVpnService getInstance() {
        return sInstance;
    }

    public static void setCurrentSocksAddr(String addr) {
        if (addr != null && !addr.isEmpty()) {
            currentSocksAddr = addr;
        }
    }

    public static String getCurrentSocksAddr() {
        return currentSocksAddr;
    }

    public static boolean isPermissionGranted(Context context) {
        return VpnService.prepare(context) == null;
    }

    /**
     * 由 Go 侧通过 JNI 调用。启动（或在未授权时弹出系统对话框后启动）VPN 服务。
     */
    public static void startVpnService(Context context, String socksAddr) {
        setCurrentSocksAddr(socksAddr);
        Intent intent = VpnService.prepare(context);
        if (intent != null) {
            // 尚未授权 VPN 权限，弹出系统授权对话框。
            // 宿主 Activity 必须在 onActivityResult 里调用 startServiceAfterPermission(this)。
            if (context instanceof Activity) {
                try {
                    ((Activity) context).startActivityForResult(intent, REQUEST_CODE_PREPARE);
                } catch (Throwable t) {
                    Log.e(TAG, "startActivityForResult failed", t);
                }
            } else {
                Log.e(TAG, "VPN permission is required, but no Activity is available");
            }
        } else {
            // 已授权，直接启动服务
            startServiceInternal(context);
        }
    }

    /**
     * 宿主 Activity.onActivityResult 中调用：权限授予后再启动服务。
     */
    public static void startServiceAfterPermission(Context context) {
        Intent intent = VpnService.prepare(context);
        if (intent == null) {
            startServiceInternal(context);
        } else {
            Log.w(TAG, "VPN permission not granted yet, service not started");
        }
    }

    /**
     * 由 Go 侧通过 JNI 调用。停止 VPN 服务。
     */
    public static void stopVpnService(Context context) {
        try {
            Intent intent = new Intent(context, GoOutVpnService.class);
            intent.setAction("STOP");
            context.stopService(intent);
        } catch (Throwable t) {
            Log.e(TAG, "Failed to stop VPN service", t);
        }
    }

    private static void startServiceInternal(Context context) {
        Intent intent = new Intent(context, GoOutVpnService.class);
        try {
            context.startService(intent);
        } catch (Throwable t) {
            Log.e(TAG, "Failed to start VPN service", t);
        }
    }

    @Override
    public void onCreate() {
        super.onCreate();
        sInstance = this;
        Log.i(TAG, "onCreate: service created, native library loaded");
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent != null && "STOP".equals(intent.getAction())) {
            stopVpn();
            return START_NOT_STICKY;
        }
        startVpn();
        return START_STICKY;
    }

    private void startVpn() {
        if (mInterface != null) {
            Log.i(TAG, "startVpn: interface already established");
            return;
        }
        try {
            Builder builder = new Builder();
            builder.setSession("GoOut")
                    .addAddress("10.0.0.2", 24)
                    .addRoute("0.0.0.0", 0)
                    .addDnsServer("8.8.8.8");

            mInterface = builder.establish();
            if (mInterface == null) {
                Log.e(TAG, "startVpn: failed to establish VPN interface");
                return;
            }
            int fd = mInterface.getFd();
            Log.i(TAG, "startVpn: VPN interface established, FD=" + fd + ", socks=" + currentSocksAddr);
            // 关键：把真实 FD 传回 Go 侧
            try {
                nativeStartVPNWithFD(fd, currentSocksAddr);
            } catch (Throwable t) {
                Log.e(TAG, "nativeStartVPNWithFD failed", t);
            }
        } catch (Throwable t) {
            Log.e(TAG, "startVpn: exception", t);
        }
    }

    private void stopVpn() {
        closeInterface();
        stopSelf();
    }

    private void closeInterface() {
        if (mInterface != null) {
            try {
                mInterface.close();
            } catch (IOException e) {
                Log.e(TAG, "closeInterface: error closing", e);
            }
            mInterface = null;
        }
    }

    @Override
    public void onDestroy() {
        super.onDestroy();
        closeInterface();
        sInstance = null;
    }

    // 与 Go side //export Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD 对应
    public native void nativeStartVPNWithFD(int fd, String socksAddr);
}