// Package assets embeds prebuilt Android bootstrap files used by every APK.
//
//	classes.dex     ← android/src/com/gofront/app/MainActivity.java
//	resources.arsc  ← android/AndroidManifest.xml (via aapt2)
//
// Rebuild: ./tools/regen-android-assets.sh
package assets

import _ "embed"

//go:embed resources.arsc
var ResourcesArsc []byte

//go:embed classes.dex
var ClassesDex []byte
