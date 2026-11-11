#!/bin/bash
# 报错时立即停止脚本执行
set -e

export ANDROID_HOME=$HOME/android-sdk
export ANDROID_NDK_HOME=$HOME/android-ndk-r25c
export PATH=$PATH:$(pwd)

# 检查 Icon.png 是否存在
if [ ! -f "Icon.png" ]; then
    echo "❌ 错误: 未在当前目录找到 Icon.png 图标文件！"
    exit 1
fi

# 检查 bundletool.jar 是否存在
if [ ! -f "bundletool.jar" ]; then
    echo "❌ 错误: 未在当前目录找到 bundletool.jar！"
    echo "请前往 https://github.com/google/bundletool/releases 下载并放入当前目录。"
    exit 1
fi

rm -f goout.aab

echo "📦 正在使用 Fyne 编译 AAB..."
fyne release --os android/amd64 \
             --appID com.gzjjjfree.goout \
             --icon Icon.png \
             --keyStore upload-keystore.jks \
             --keyStorePass "61354439" \
             --keyPass "61354439" \
             --appVersion "1.0.0" \
             --appBuild "1" \
             --keyName "goout-alias" \
             --name goout

rm -f goout.apks
rm -f goout.apk

echo "⚙️ 正在使用 bundletool 转换为 APKS..."
./bundletool build-apks --bundle=goout.aab \
                        --output=goout.apks \
                        --mode=universal \
                        --ks=upload-keystore.jks \
                        --ks-pass=pass:"61354439" \
                        --ks-key-alias="goout-alias" \
                        --key-pass=pass:"61354439"

echo "🔓 正在解压提取通用 APK..."
unzip -p goout.apks universal.apk > goout.apk

echo "确保生成的 APK 具备基本大小（解除 0 字节假成功）"
if [ -s "goout.apk" ]; then
    echo "========= 🎉 打包成功 ========="
    echo "请检查当前目录下的 goout.apk"
else
    echo "❌ 错误: 提取出来的 goout.apk 是空文件！"
    exit 1
fi

# keytool -genkeypair \
#         -v \
#         -keystore upload-keystore.jks \
#         -alias goout-alias \
#         -keyalg RSA \
#         -keysize 2048 \
#         -validity 10000

# base64 -w 0 upload-keystore.jks