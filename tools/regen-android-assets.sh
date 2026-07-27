#!/usr/bin/env bash
# Rebuild internal/assets/{classes.dex,resources.arsc} from android/.
# Needs: javac, curl, unzip. Downloads android.jar + build-tools into .cache/android/.
# Usage: ./tools/regen-android-assets.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="$ROOT/.cache/android"
OUT="$ROOT/internal/assets"

API=28
BUILD_TOOLS_URL="https://dl.google.com/android/repository/build-tools_r34-linux.zip"
ANDROID_JAR_URL="https://raw.githubusercontent.com/Sable/android-platforms/master/android-${API}/android.jar"

mkdir -p "$CACHE"

if [ ! -f "$CACHE/android.jar" ]; then
  echo "==> downloading android.jar (API $API)"
  curl -fL -o "$CACHE/android.jar" "$ANDROID_JAR_URL"
fi

BT="$CACHE/bt/android-14"
if [ ! -x "$BT/d8" ]; then
  echo "==> downloading Android build-tools"
  curl -fL -o "$CACHE/bt.zip" "$BUILD_TOOLS_URL"
  rm -rf "$CACHE/bt"
  unzip -q -o "$CACHE/bt.zip" -d "$CACHE/bt"
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> javac"
mkdir -p "$WORK/classes"
javac --release 8 -classpath "$CACHE/android.jar" -d "$WORK/classes" \
  $(find "$ROOT/android/src" -name '*.java')

echo "==> d8 -> classes.dex"
"$BT/d8" --min-api 21 --lib "$CACHE/android.jar" --output "$WORK" \
  $(find "$WORK/classes" -name '*.class')

echo "==> aapt2 link -> resources.arsc"
"$BT/aapt2" link -o "$WORK/base.apk" -I "$CACHE/android.jar" \
  --manifest "$ROOT/android/AndroidManifest.xml" \
  --min-sdk-version 21 --target-sdk-version "$API"
unzip -q -o "$WORK/base.apk" resources.arsc -d "$WORK/unz"

cp "$WORK/classes.dex"        "$OUT/classes.dex"
cp "$WORK/unz/resources.arsc" "$OUT/resources.arsc"

echo "==> updated:"
ls -la "$OUT"
