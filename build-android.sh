#!/usr/bin/env bash
set -e

echo "Building GoOut for Android using fyne-cross..."
fyne-cross android -arch arm64-v8a --pull=false
echo "Android build complete!"
