package com.gzjjjfree.goout;

import android.app.Activity;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.os.ParcelFileDescriptor;
import android.util.Log;
import java.io.IOException;
import java.net.InetAddress;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;

/**
 * Android VpnService 宿主：负责调起系统 VPN、建立 TUN 接口，
 * 支持按 CIDR 排除特定网段/IP（如 8.8.8.8/32），并通过 JNI 把真实 fd 传回 Go 侧。
 */
public class GoOutVpnService extends VpnService {
    private static final String TAG = "GoOutVpnService";
    public static final int REQUEST_VPN_PERMISSION = 0x1001;

    private ParcelFileDescriptor mInterface;
    private static GoOutVpnService sInstance;
    private static String currentSocksAddr = "127.0.0.1:10909";

    // 默认排除私有/保留/环回/组播等非公网网段，使 VPN 仅接管对外公网 IP 流量
    private static List<String> excludedCidrs = Arrays.asList(
            "0.0.0.0/8",
            "10.0.0.0/8",
            "100.64.0.0/10",
            "127.0.0.0/8",
            "169.254.0.0/16",
            "172.16.0.0/12",
            "192.168.0.0/16",
            "224.0.0.0/4",
            "240.0.0.0/4"
    );

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
     * 设置需要排除出 TUN 路由的 IPv4 地址或 CIDR 列表（例如 "8.8.8.8/32", "192.168.1.0/24"）。
     */
    public static void setExcludedCidrs(List<String> cidrs) {
        if (cidrs == null || cidrs.isEmpty()) {
            excludedCidrs = Collections.emptyList();
            return;
        }
        List<String> normalized = new ArrayList<>();
        for (String item : cidrs) {
            if (item == null) {
                continue;
            }
            String trimmed = item.trim();
            if (trimmed.isEmpty()) {
                continue;
            }
            if (!trimmed.contains("/")) {
                trimmed = trimmed + "/32";
            }
            normalized.add(trimmed);
        }
        excludedCidrs = normalized;
    }

    public static void setExcludedCidrs(String... cidrs) {
        if (cidrs == null) {
            setExcludedCidrs(Collections.emptyList());
            return;
        }
        setExcludedCidrs(Arrays.asList(cidrs));
    }

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
                    .addAddress("10.0.0.2", 24);

            // 仅接管排除私有/保留/环回/组播网段后的公网路由，不指定 8.8.8.8 DNS
            addRoutesExcluding(builder, excludedCidrs);

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

    /**
     * 将 [0.0.0.0, 255.255.255.255] 扣除所有 excludedCidrs 后，拆分为若干不重叠的 CIDR 并添加到 Builder。
     */
    private void addRoutesExcluding(Builder builder, List<String> cidrs) {
        List<long[]> excludedRanges = new ArrayList<>();
        if (cidrs != null) {
            for (String cidr : cidrs) {
                long[] range = parseIpv4CidrToRange(cidr);
                if (range != null) {
                    excludedRanges.add(range);
                } else {
                    Log.w(TAG, "Ignoring invalid IPv4 CIDR in exclusion list: " + cidr);
                }
            }
        }

        if (excludedRanges.isEmpty()) {
            builder.addRoute("0.0.0.0", 0);
            Log.i(TAG, "Added default route 0.0.0.0/0 (no excluded CIDRs)");
            return;
        }

        // 合并重叠或相邻的排除区间
        excludedRanges.sort((a, b) -> {
            if (a[0] != b[0]) {
                return Long.compare(a[0], b[0]);
            }
            return Long.compare(a[1], b[1]);
        });

        List<long[]> merged = new ArrayList<>();
        for (long[] cur : excludedRanges) {
            if (merged.isEmpty()) {
                merged.add(new long[]{cur[0], cur[1]});
            } else {
                long[] last = merged.get(merged.size() - 1);
                if (cur[0] <= last[1] + 1L) {
                    if (cur[1] > last[1]) {
                        last[1] = cur[1];
                    }
                } else {
                    merged.add(new long[]{cur[0], cur[1]});
                }
            }
        }

        // 计算 [0, 0xFFFFFFFFL] 补集并转换为 CIDR 路由
        long cursor = 0L;
        final long maxIp = 0xFFFFFFFFL;
        int addedCount = 0;

        for (long[] ex : merged) {
            long exStart = Math.max(0L, ex[0]);
            long exEnd = Math.min(maxIp, ex[1]);
            if (exStart > cursor) {
                addedCount += addRangeAsCidrs(builder, cursor, exStart - 1L);
            }
            if (exEnd >= maxIp) {
                cursor = maxIp + 1L;
                break;
            }
            cursor = exEnd + 1L;
        }

        if (cursor <= maxIp) {
            addedCount += addRangeAsCidrs(builder, cursor, maxIp);
        }

        Log.i(TAG, "Added " + addedCount + " TUN routes excluding " + cidrs);
    }

    private int addRangeAsCidrs(Builder builder, long start, long end) {
        int count = 0;
        long cur = start;
        while (cur <= end) {
            int maxAlignBits = 32;
            if (cur != 0L) {
                int trailingZeros = Long.numberOfTrailingZeros(cur);
                maxAlignBits = Math.min(32, trailingZeros);
            }
            long remaining = end - cur + 1L;
            int fitBits = 63 - Long.numberOfLeadingZeros(remaining);
            int prefixLen = 32 - Math.min(maxAlignBits, fitBits);
            long blockSize = 1L << (32 - prefixLen);

            InetAddress addr = longToIpv4Address(cur);
            if (addr != null) {
                builder.addRoute(addr, prefixLen);
                count++;
            }

            if (cur + blockSize - 1L == end || blockSize == (1L << 32)) {
                break;
            }
            cur += blockSize;
        }
        return count;
    }

    private long[] parseIpv4CidrToRange(String cidr) {
        if (cidr == null || cidr.isEmpty()) {
            return null;
        }
        String[] parts = cidr.split("/");
        if (parts.length != 2) {
            return null;
        }
        int prefix;
        try {
            prefix = Integer.parseInt(parts[1].trim());
        } catch (NumberFormatException e) {
            return null;
        }
        if (prefix < 0 || prefix > 32) {
            return null;
        }
        long ip = ipv4StringToLong(parts[0].trim());
        if (ip < 0) {
            return null;
        }
        long mask = prefix == 0 ? 0L : (~0L << (32 - prefix)) & 0xFFFFFFFFL;
        long start = ip & mask;
        long end = start | ((~mask) & 0xFFFFFFFFL);
        return new long[]{start, end};
    }

    private long ipv4StringToLong(String ipStr) {
        String[] octets = ipStr.split("\\.");
        if (octets.length != 4) {
            return -1L;
        }
        long result = 0L;
        for (String o : octets) {
            int val;
            try {
                val = Integer.parseInt(o);
            } catch (NumberFormatException e) {
                return -1L;
            }
            if (val < 0 || val > 255) {
                return -1L;
            }
            result = (result << 8) | (val & 0xFFL);
        }
        return result & 0xFFFFFFFFL;
    }

    private InetAddress longToIpv4Address(long ip) {
        byte[] bytes = new byte[]{
                (byte) ((ip >>> 24) & 0xFF),
                (byte) ((ip >>> 16) & 0xFF),
                (byte) ((ip >>> 8) & 0xFF),
                (byte) (ip & 0xFF)
        };
        try {
            return InetAddress.getByAddress(bytes);
        } catch (Exception e) {
            return null;
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
