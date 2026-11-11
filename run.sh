if adb -H 127.0.0.1 install -r goout.apk; then
    echo "安装成功！"
else
    echo "签名不匹配，尝试卸载后重新安装..."
    adb -H 127.0.0.1 uninstall com.gzjjjfree.goout
    adb -H 127.0.0.1 install goout.apk
fi