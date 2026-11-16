//go:build android

package statusbar

/*
#cgo LDFLAGS: -landroid -llog
#include <jni.h>
#include <android/log.h>

void setLightStatusBar(uintptr_t envPtr, uintptr_t activityPtr) {
    JNIEnv *env = (JNIEnv *)envPtr;
    jobject activity = (jobject)activityPtr;

    if (!env || !activity) {
        __android_log_print(ANDROID_LOG_ERROR, "StatusBar", "JNIEnv or Activity is NULL");
        return;
    }

    // Get Build.VERSION.SDK_INT
    jclass versionClass = (*env)->FindClass(env, "android/os/Build$VERSION");
    if (!versionClass) {
        __android_log_print(ANDROID_LOG_ERROR, "StatusBar", "Failed to find Build.VERSION");
        return;
    }
    jfieldID sdkIntField = (*env)->GetStaticFieldID(env, versionClass, "SDK_INT", "I");
    if (!sdkIntField) {
        __android_log_print(ANDROID_LOG_ERROR, "StatusBar", "Failed to find SDK_INT field");
        return;
    }
    jint sdkInt = (*env)->GetStaticIntField(env, versionClass, sdkIntField);
    __android_log_print(ANDROID_LOG_INFO, "StatusBar", "Android SDK_INT: %d", sdkInt);

    // Get Window: activity.getWindow()
    jclass activityClass = (*env)->GetObjectClass(env, activity);
    jmethodID getWindowMethod = (*env)->GetMethodID(env, activityClass, "getWindow", "()Landroid/view/Window;");
    if (!getWindowMethod) {
        __android_log_print(ANDROID_LOG_ERROR, "StatusBar", "Failed to find getWindow method");
        return;
    }
    jobject window = (*env)->CallObjectMethod(env, activity, getWindowMethod);
    if (!window) {
        __android_log_print(ANDROID_LOG_ERROR, "StatusBar", "Window object is NULL");
        return;
    }

    if (sdkInt >= 30) {
        // SDK >= 30 (Android 11+): WindowInsetsController controller = window.getInsetsController();
        // controller.setSystemBarsAppearance(APPEARANCE_LIGHT_STATUS_BARS, APPEARANCE_LIGHT_STATUS_BARS);
        jclass windowClass = (*env)->GetObjectClass(env, window);
        jmethodID getInsetsControllerMethod = (*env)->GetMethodID(env, windowClass, "getInsetsController", "()Landroid/view/WindowInsetsController;");
        if (getInsetsControllerMethod) {
            jobject insetsController = (*env)->CallObjectMethod(env, window, getInsetsControllerMethod);
            if (insetsController) {
                jclass controllerClass = (*env)->GetObjectClass(env, insetsController);
                // APPEARANCE_LIGHT_STATUS_BARS = 8
                jmethodID setSystemBarsAppearanceMethod = (*env)->GetMethodID(env, controllerClass, "setSystemBarsAppearance", "(II)V");
                if (setSystemBarsAppearanceMethod) {
                    (*env)->CallVoidMethod(env, insetsController, setSystemBarsAppearanceMethod, 8, 8);
                    __android_log_print(ANDROID_LOG_INFO, "StatusBar", "Applied light status bar via WindowInsetsController (API 30+)");
                }
            }
        }
    } else if (sdkInt >= 23) {
        // 23 <= SDK < 30 (Android 6.0 - 10): View decorView = window.getDecorView();
        // int vis = decorView.getSystemUiVisibility();
        // decorView.setSystemUiVisibility(vis | SYSTEM_UI_FLAG_LIGHT_STATUS_BAR);
        // SYSTEM_UI_FLAG_LIGHT_STATUS_BAR = 0x00002000 (8192)
        jclass windowClass = (*env)->GetObjectClass(env, window);
        jmethodID getDecorViewMethod = (*env)->GetMethodID(env, windowClass, "getDecorView", "()Landroid/view/View;");
        if (getDecorViewMethod) {
            jobject decorView = (*env)->CallObjectMethod(env, window, getDecorViewMethod);
            if (decorView) {
                jclass viewClass = (*env)->GetObjectClass(env, decorView);
                jmethodID getVisMethod = (*env)->GetMethodID(env, viewClass, "getSystemUiVisibility", "()I");
                jmethodID setVisMethod = (*env)->GetMethodID(env, viewClass, "setSystemUiVisibility", "(I)V");
                if (getVisMethod && setVisMethod) {
                    jint vis = (*env)->CallIntMethod(env, decorView, getVisMethod);
                    jint lightStatusBarFlag = 0x00002000; // SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
                    (*env)->CallVoidMethod(env, decorView, setVisMethod, vis | lightStatusBarFlag);
                    __android_log_print(ANDROID_LOG_INFO, "StatusBar", "Applied light status bar via setSystemUiVisibility (API 23-29)");
                }
            }
        }
    }
}
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// AdaptStatusBar applies light status bar styling (dark icons) on Android devices.
func AdaptStatusBar(a fyne.App) {
	apply := func() {
		_ = driver.RunNative(func(ctx any) error {
			if ac, ok := ctx.(*driver.AndroidContext); ok {
				C.setLightStatusBar(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx))
			}
			return nil
		})
	}

	apply()
	a.Lifecycle().SetOnStarted(func() {
		apply()
	})
	a.Lifecycle().SetOnEnteredForeground(func() {
		apply()
	})
}
