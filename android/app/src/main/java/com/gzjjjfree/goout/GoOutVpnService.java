package com.gzjjjfree.goout;

import android.app.Activity;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.ParcelFileDescriptor;
import android.util.Log;
import java.io.IOException;

/**
 * Android VpnService 宿主：负责调起系统 VPN、建立 TUN 接口，
 * 提供 Socket 保护接口（protect），并通过 JNI 把真实 fd 传回 Go 侧。
 */
public class GoOutVpnService extends VpnService {
    private static final String TAG = "GoOutVpnService";
    public static final int REQUEST_VPN_PERMISSION = 0x1001;

    private ParcelFileDescriptor mInterface;
    private static GoOutVpnService sInstance;
    private static String currentSocksAddr = "127.0.0.1:10909";

    static {
        try {
            System.loadLibrary("GoOut");
        } catch (Throwable t) {
            Log.e(TAG, "Failed to load native library GoOut in static block", t);
        }
    }

    public GoOutVpnService() {
        try {
            System.loadLibrary("GoOut");
        } catch (Throwable t) {
            Log.e(TAG, "Failed to load native library GoOut in constructor", t);
        }
    }

    public static GoOutVpnService getInstance() {
        return sInstance;
    }

    /**
     * 供 JNI 调用：将出站 Socket FD 排除出 VPN 路由，使其走底层物理网卡。
     */
    public static boolean protectFd(int fd) {
        if (fd < 0) {
            Log.w(TAG, "protectFd: invalid fd=" + fd);
            return false;
        }
        GoOutVpnService svc = sInstance;
        if (svc != null) {
            boolean ok = svc.protect(fd);
            Log.i(TAG, "protectFd: fd=" + fd + ", success=" + ok);
            return ok;
        }
        Log.w(TAG, "protectFd: GoOutVpnService instance is null, fd=" + fd);
        return false;
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
        if (context == null) {
            return false;
        }
        try {
            return VpnService.prepare(context) == null;
        } catch (Throwable t) {
            Log.e(TAG, "isPermissionGranted error", t);
            return false;
        }
    }

    /**
     * 由 Go 侧通过 JNI 调用。启动（或在未授权时弹出系统对话框后启动）VPN 服务。
     */
    public static void startVpnService(Context context, String socksAddr) {
        if (context == null) {
            Log.e(TAG, "startVpnService: context is null");
            return;
        }
        setCurrentSocksAddr(socksAddr);

        Intent prepareIntent;
        try {
            prepareIntent = VpnService.prepare(context);
        } catch (Throwable t) {
            Log.e(TAG, "VpnService.prepare failed", t);
            return;
        }

        if (prepareIntent != null) {
            Log.i(TAG, "VPN permission intent required, context=" + context.getClass().getName());
            try {
                if (context instanceof Activity) {
                    Activity act = (Activity) context;
                    if (act.isFinishing() || act.isDestroyed()) {
                        Log.w(TAG, "Host Activity is finishing/destroyed");
                        return;
                    }
                    act.startActivityForResult(prepareIntent, REQUEST_VPN_PERMISSION);
                    Log.i(TAG, "VPN permission intent launched via Activity.startActivityForResult");
                } else {
                    prepareIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
                    context.startActivity(prepareIntent);
                    Log.i(TAG, "VPN permission intent launched via Context.startActivity(NEW_TASK)");
                }
            } catch (Throwable t) {
                Log.e(TAG, "Failed to start VPN permission activity", t);
            }
        } else {
            Log.i(TAG, "VPN permission already granted, starting GoOutVpnService");
            startServiceInternal(context);
        }
    }

    /**
     * 供宿主 Activity 的 onActivityResult 调用。
     */
    public static void onVpnPermissionResult(Context context, int requestCode, int resultCode) {
        if (requestCode != REQUEST_VPN_PERMISSION) {
            return;
        }
        if (resultCode == Activity.RESULT_OK) {
            Log.i(TAG, "VPN permission granted by user, starting GoOutVpnService");
            startServiceInternal(context);
        } else {
            Log.w(TAG, "VPN permission denied or cancelled, resultCode=" + resultCode);
            nativeOnVpnPermissionDenied();
        }
    }

    /**
     * 由 Go 侧通过 JNI 调用。停止 VPN 服务。
     */
    public static void stopVpnService(Context context) {
        if (context == null) {
            Log.e(TAG, "stopVpnService: context is null");
            return;
        }
        try {
            Intent intent = new Intent(context, GoOutVpnService.class);
            intent.setAction("STOP");
            context.startService(intent);
        } catch (Throwable t) {
            Log.e(TAG, "Failed to stop VPN service", t);
        }
    }

    private static void startServiceInternal(Context context) {
        if (context == null) {
            return;
        }
        Intent intent = new Intent(context, GoOutVpnService.class);
        try {
            ComponentName cn = context.startService(intent);
            Log.i(TAG, "startService called, ComponentName=" + cn);
        } catch (Throwable t) {
            Log.e(TAG, "Failed to start GoOutVpnService", t);
        }
    }

    @Override
    public void onCreate() {
        super.onCreate();
        sInstance = this;
        Log.i(TAG, "onCreate: GoOutVpnService created");
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        Log.i(TAG, "onStartCommand: action=" + (intent != null ? intent.getAction() : "null") + ", startId=" + startId);
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
                Log.e(TAG, "startVpn: builder.establish() returned null");
                nativeOnVpnPermissionDenied();
                return;
            }
            int fd = mInterface.detachFd();
            Log.i(TAG, "startVpn: VPN interface established, detached FD=" + fd + ", socks=" + currentSocksAddr);
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
        Log.i(TAG, "stopVpn called");
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
        Log.i(TAG, "onDestroy");
        super.onDestroy();
        closeInterface();
        sInstance = null;
    }

    public native void nativeStartVPNWithFD(int fd, String socksAddr);

    public static native void nativeOnVpnPermissionDenied();
}
