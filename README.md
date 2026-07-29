# gofront

Perfect for developers who want to create Android applications with only Golang and HTML+JS+CSS

Go + any JS frontend → one Android `.apk`.

On the phone a tiny WebView opens your UI; a Go binary serves `frontend/` and
answers RPC calls from JS.

## Install

Needs [Go](https://go.dev/dl/) 1.25+ (`$GOPATH/bin` or `$HOME/go/bin` on your `PATH`).

```sh
go install github.com/ktago336/gofront/cmd/gofront@latest
```

Check:

```sh
gofront help
```

From a clone of this repo:

```sh
git clone https://github.com/ktago336/gofront.git
cd gofront
go install ./cmd/gofront
```

Optional: [`adb`](https://developer.android.com/tools/adb) for `-install` / `-run`.

## Quick start (example app)

Phone on USB with USB debugging on:

```sh
git clone https://github.com/ktago336/gofront.git
cd gofront
go run ./cmd/gofront build ./example -install -run
```

Builds, installs, and launches the sample APK. Needs Go and `adb`.

APK only:

```sh
go run ./cmd/gofront build ./example -o app.apk
```

## Write an app

In your module:

```sh
go get github.com/ktago336/gofront@latest
```

```go
package main

import "github.com/ktago336/gofront"

type API struct{}

func (a *API) Hello(name string) string { return "Hello, " + name }

func main() {
	app := gofront.New()
	app.Bind("api", &API{})
	app.Run()
}
```

Put UI in `frontend/` (see `example/frontend/`):

```html
<script src="/gofront.js"></script>
<script>
  const msg = await window.gofront.api.Hello("WebView");
</script>
```

```sh
gofront build . -o app.apk
gofront build . -install -run
```

### Custom AndroidManifest

Write the default manifest (same as `build` without `-override-manifest`), edit
permissions / metadata, then build with the override:

```sh
gofront init-manifest
# edit AndroidManifest.xml — e.g. add CAMERA for getUserMedia
gofront build . -override-manifest AndroidManifest.xml -install -run
```

`init-manifest` flags: `-o` output path (default `<dir>/AndroidManifest.xml`),
`-f` overwrite if the file exists. Optional `[dir]` defaults to `.`.

Keep activity `com.gofront.app.MainActivity` — that class is what the embedded
bootstrap provides.

### `build` flags

| flag | default | meaning |
|------|---------|---------|
| `-o` | `app.apk` | output path |
| `-frontend` | `<pkg>/frontend` | dir to bundle and serve |
| `-abi` | `arm64-v8a` | target ABI |
| `-package` | `com.gofront.app` | application id |
| `-label` | `GoFront` | display name |
| `-version-code` | `1` | versionCode |
| `-version-name` | `1.0` | versionName |
| `-min-sdk` | `21` | minSdkVersion |
| `-target-sdk` | `28` | targetSdkVersion |
| `-icon` | | PNG path for the launcher icon |
| `-override-manifest` | | custom `AndroidManifest` (binary AXML or XML) |
| `-install` | | install with adb |
| `-run` | | launch after install |

`-icon`: PNG is packed as `@mipmap/ic_launcher` via `aapt2` (same cache as
manifest compile). Works with `-override-manifest` when that file is XML; binary
AXML overrides are not supported together with `-icon`.

`-override-manifest`: binary AXML is used as-is; XML is compiled with `aapt2`
(auto-downloaded to the user cache, no JDK). Manifest-related flags above are
ignored when this is set (unless `-icon` is also set, in which case the XML is
re-linked with the icon resources).

## How it works

1. Cross-compile Go (`GOOS=linux`, `CGO_ENABLED=0`) → `lib/<abi>/libserver.so`
2. Bootstrap dex starts that binary and loads `http://127.0.0.1:8080` in a WebView
3. `Bind` + reflection → `gofront.js` / `gofront.d.ts`; calls hit `POST /gofront/call`
4. Manifest generated in Go (unless overridden); signed v1+v2+v3 via [apksig-go](https://github.com/agusibrahim/apksig-go)

No Android SDK/NDK/JDK for a normal build — only the Go toolchain.

## Layout

| path | role |
|------|------|
| `gofront.go`, `bindings.go` | library |
| `cmd/gofront` | CLI |
| `internal/apk` | pack + sign + manifest |
| `internal/assets/` | embedded `classes.dex`, `resources.arsc` |
| `android/` | sources for those embeds |
| `tools/regen-android-assets.sh` | rebuild embeds |
| `example/` | sample app |

### Embedded binaries

Checked in so users do not need a JDK. Sources are next to them:

| file | source |
|------|--------|
| `internal/assets/classes.dex` | `android/src/com/gofront/app/MainActivity.java` |
| `internal/assets/resources.arsc` | from `android/AndroidManifest.xml` via aapt2 |

Per-app APK manifest is **not** embedded; it is generated in
`internal/apk/axml.go` (or taken from `-override-manifest`).

Rebuild embeds (needs `javac`, `curl`, `unzip`; downloads tooling into `.cache/`):

```sh
./tools/regen-android-assets.sh
go install ./cmd/gofront
```

## License

MIT. APK signing uses [apksig-go](https://github.com/agusibrahim/apksig-go) (Apache-2.0).

## Notes

- Activity class is fixed: `com.gofront.app.MainActivity` (package id is a flag).
- If install fails on a locked-down phone, try disabling Play Protect / “verify apps
  over USB”, or install manually with
  `adb install --bypass-low-target-sdk-block app.apk`.
