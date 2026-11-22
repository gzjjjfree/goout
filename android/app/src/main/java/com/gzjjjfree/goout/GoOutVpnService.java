package com.gzjjjfree.goout;

import android.app.Activity;
import android.app.Application;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.Bundle;
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
    private static String currentSocksAddr = "127.0.0.1:10909";

    private static volatile boolean waitingForPermission = false;
    private static final Application.ActivityLifecycleCallbacks RESUME_CALLBACK =
            new Application.ActivityLifecycleCallbacks() {
        @Override public void onActivityCreated(Activity a, Bundle s) {}
        @Override public void onActivityStarted(Activity a) {}
        @Override public void onActivityResumed(Activity a) {
            if (!waitingForPermission) return;
            waitingForPermission = false;
            try {
                Application app = (Application) a.getApplicationContext();
                app.unregisterActivityLifecycleCallbacks(RESUME_CALLBACK);
            } catch (Throwable t) {}
            Intent intent = VpnService.prepare(a);
            if (intent == null) {
                startServiceInternal(a);
            } else {
                Log.w(TAG, "Permission still not granted after dialog");
            }
        }
        @Override public void onActivityPaused(Activity a) {}
        @Override public void onActivityStopped(Activity a) {}
        @Override public void onActivitySaveInstanceState(Activity a, Bundle s) {}
        @Override public void onActivityDestroyed(Activity a) {}
    };

    static {
        System.loadLibrary("GoOut");
    }

    public GoOutVpnService() {
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
        Activity activity = currentForegroundActivity(context);
        Context prepareCtx = (activity != null) ? activity : context;
        return VpnService.prepare(prepareCtx) == null;
    }

    /**
     * 由 Go 侧通过 JNI 调用。启动（或在未授权时弹出系统对话框后启动）VPN 服务。
     */
    public static void startVpnService(Context context, String socksAddr) {
        setCurrentSocksAddr(socksAddr);
        Activity activity = currentForegroundActivity(context);
        Context prepareCtx = (activity != null) ? activity : context;

        Intent intent = VpnService.prepare(prepareCtx);
        if (intent != null) {
            if (activity != null) {
                waitingForPermission = true;
                final Activity act = activity;
                act.runOnUiThread(new Runnable() {
                    @Override public void run() {
                        try {
                            Application app = (Application) act.getApplicationContext();
                            app.registerActivityLifecycleCallbacks(RESUME_CALLBACK);
                            act.startActivityForResult(intent, REQUEST_CODE_PREPARE);
                        } catch (Throwable t) {
                            Log.e(TAG, "startActivityForResult failed", t);
                            waitingForPermission = false;
                            try {
                                Application app = (Application) act.getApplicationContext();
                                app.unregisterActivityLifecycleCallbacks(RESUME_CALLBACK);
                            } catch (Throwable ignored) {}
                        }
                    }
                });
            } else {
                Log.e(TAG, "VPN permission is required, but no Activity is available");
            }
        } else {
            startServiceInternal(prepareCtx);
        }
    }

    private static Activity currentForegroundActivity(Context context) {
        try {
            Class<?> at = Class.forName("android.app.ActivityThread");
            java.lang.reflect.Method m = at.getMethod("currentActivity");
            m.setAccessible(true);
            Object act = m.invoke(null);
            if (act instanceof Activity) {
                return (Activity) act;
            }
        } catch (Throwable t) {
            Log.e(TAG, "currentActivity reflection failed", t);
        }
        if (context instanceof Activity) {
            return (Activity) context;
        }
        return null;
    }

    /**
     * 宿主 Activity.onActivityResult 中调用：权限授予后再启动服务。
     */
    public static void startServiceAfterPermission(Context context) {
        Activity activity = currentForegroundActivity(context);
        Context prepareCtx = (activity != null) ? activity : context;
        Intent intent = VpnService.prepare(prepareCtx);
        if (intent == null) {
            startServiceInternal(prepareCtx);
        } else {
            Log.w(TAG, "VPN permission not granted yet, service not started");
        }
    }

    /**
     * 由 Go 侧通过 JNI 调用。停止 VPN 服务。
     */
    public static void stopVpnService(Context context) {
        try {
            Activity activity = currentForegroundActivity(context);
            Context ctx = (activity != null) ? activity : context;
            Intent intent = new Intent(ctx, GoOutVpnService.class);
            intent.setAction("STOP");
            ctx.stopService(intent);
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
