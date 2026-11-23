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
 * 并通过 JNI 把真实 fd 传回 Go 侧，由 tun2socks 接管流量。
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

    public static boolean protectFd(int fd) {
        if (sInstance != null) {
            boolean success = sInstance.protect(fd);
            Log.i(TAG, "protectFd: fd=" + fd + ", success=" + success);
            return success;
        }
        Log.w(TAG, "protectFd: sInstance is null, cannot protect fd=" + fd);
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
            Log.i(TAG, "VPN permission intent required, context=" + context.getClass().getName()
                    + ", intent=" + prepareIntent);
            try {
                if (context instanceof Activity) {
                    Activity act = (Activity) context;
                    if (act.isFinishing() || act.isDestroyed()) {
                        Log.w(TAG, "Host Activity is finishing/destroyed, cannot launch permission dialog");
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
     * 供宿主 Activity 的 onActivityResult 调用，在用户授权后自动启动服务；
     * 若用户拒绝或系统取消，则通知 Go 侧取消等待状态。
     */
    public static void onVpnPermissionResult(Context context, int requestCode, int resultCode) {
        if (requestCode != REQUEST_VPN_PERMISSION) {
            return;
        }
        if (resultCode == Activity.RESULT_OK) {
            Log.i(TAG, "VPN permission granted by user, starting GoOutVpnService");
            startServiceInternal(context);
        } else {
            Log.w(TAG, "VPN permission denied or cancelled by user, resultCode=" + resultCode);
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
            Log.i(TAG, "startVpn: interface already established, fd=" + mInterface.getFd());
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