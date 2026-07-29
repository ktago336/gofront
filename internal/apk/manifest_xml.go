package apk

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultManifestParams are the values used by gofront build when the
// corresponding flags are left at their defaults.
func DefaultManifestParams() ManifestParams {
	return ManifestParams{
		Package:       "com.gofront.app",
		Label:         "GoFront",
		VersionCode:   1,
		VersionName:   "1.0",
		MinSDK:        21,
		TargetSDK:     28,
		ActivityClass: "com.gofront.app.MainActivity",
	}
}

// FormatManifestXML returns a readable AndroidManifest.xml for the same
// ManifestParams that EncodeManifest would pack into the APK. Both share
// buildManifestTree — edit that one place to change the default structure.
func FormatManifestXML(p ManifestParams) string {
	if p.ActivityClass == "" {
		p.ActivityClass = "com.gofront.app.MainActivity"
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	writeManifestElem(&b, buildManifestTree(p), 0, true)
	return b.String()
}

func writeManifestElem(b *strings.Builder, e *elem, indent int, root bool) {
	pad := strings.Repeat("    ", indent)
	b.WriteString(pad)
	b.WriteByte('<')
	b.WriteString(e.name)
	if root {
		b.WriteString(` xmlns:android="http://schemas.android.com/apk/res/android"`)
	}
	for _, a := range e.attrs {
		b.WriteByte('\n')
		b.WriteString(pad)
		b.WriteString("    ")
		if a.ns {
			b.WriteString("android:")
		}
		b.WriteString(a.name)
		b.WriteByte('=')
		fmt.Fprintf(b, "%q", attrXMLValue(a))
	}
	if len(e.children) == 0 {
		b.WriteString("/>\n")
		return
	}
	b.WriteString(">\n")
	for _, c := range e.children {
		writeManifestElem(b, c, indent+1, false)
	}
	b.WriteString(pad)
	b.WriteString("</")
	b.WriteString(e.name)
	b.WriteString(">\n")
}

func attrXMLValue(a attr) string {
	switch {
	case a.isStr:
		return a.str
	case a.typ == typeIntBoolean:
		if a.data == boolTrue {
			return "true"
		}
		return "false"
	case a.typ == typeReference && a.name == "theme" && a.data == themeNoTitleFullscreen:
		return "@android:style/Theme.NoTitleBar.Fullscreen"
	case a.typ == typeIntDec:
		return strconv.FormatUint(uint64(a.data), 10)
	case a.typ == typeReference:
		return fmt.Sprintf("@android:0x%08x", a.data)
	default:
		return strconv.FormatUint(uint64(a.data), 10)
	}
}
