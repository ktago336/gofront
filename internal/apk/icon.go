package apk

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launcherIconName = "ic_launcher"
const launcherIconRef = "@mipmap/" + launcherIconName

// iconPack is the aapt2-linked manifest + resources for a launcher icon.
type iconPack struct {
	Manifest      []byte
	ResourcesArsc []byte
	Res           []fileEntry
}

// packLauncherIcon compiles iconPath into mipmap resources via aapt2 and
// returns a manifest that references @mipmap/ic_launcher, plus resources.arsc
// and res/ entries to embed in the APK.
func packLauncherIcon(opts Options) (*iconPack, error) {
	iconData, err := os.ReadFile(opts.IconPath)
	if err != nil {
		return nil, fmt.Errorf("icon: %w", err)
	}
	if !isPNG(iconData) {
		return nil, fmt.Errorf("icon: %s is not a PNG image", opts.IconPath)
	}

	jar, aapt2, err := ensureAAPT2()
	if err != nil {
		return nil, err
	}

	work, err := os.MkdirTemp("", "gofront-icon-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	resPNG := filepath.Join(work, "res", "mipmap-xxxhdpi", launcherIconName+".png")
	if err := os.MkdirAll(filepath.Dir(resPNG), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(resPNG, iconData, 0o644); err != nil {
		return nil, err
	}

	manifestXML, err := manifestXMLForIcon(opts)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(work, "AndroidManifest.xml")
	if err := os.WriteFile(manifestPath, manifestXML, 0o644); err != nil {
		return nil, err
	}

	flatsDir := filepath.Join(work, "flats")
	if err := os.MkdirAll(flatsDir, 0o755); err != nil {
		return nil, err
	}
	compile := exec.Command(aapt2, "compile", "-o", flatsDir+string(os.PathSeparator), resPNG)
	compile.Stderr = os.Stderr
	if err := compile.Run(); err != nil {
		return nil, fmt.Errorf("aapt2 compile icon: %w", err)
	}

	flats, err := filepath.Glob(filepath.Join(flatsDir, "*.flat"))
	if err != nil {
		return nil, err
	}
	if len(flats) == 0 {
		return nil, fmt.Errorf("aapt2 compile produced no .flat files")
	}

	minSDK, targetSDK := opts.Manifest.MinSDK, opts.Manifest.TargetSDK
	if minSDK == 0 {
		minSDK = 21
	}
	if targetSDK == 0 {
		targetSDK = androidAPI
	}

	outAPK := filepath.Join(work, "res.apk")
	linkArgs := []string{
		"link", "-o", outAPK, "-I", jar,
		"--manifest", manifestPath,
		"--min-sdk-version", fmt.Sprintf("%d", minSDK),
		"--target-sdk-version", fmt.Sprintf("%d", targetSDK),
	}
	linkArgs = append(linkArgs, flats...)
	link := exec.Command(aapt2, linkArgs...)
	link.Stderr = os.Stderr
	if err := link.Run(); err != nil {
		return nil, fmt.Errorf("aapt2 link icon: %w", err)
	}

	return readIconPack(outAPK)
}

func manifestXMLForIcon(opts Options) ([]byte, error) {
	if opts.OverrideManifestPath != "" {
		raw, err := os.ReadFile(opts.OverrideManifestPath)
		if err != nil {
			return nil, fmt.Errorf("override-manifest: %w", err)
		}
		if isBinaryAXML(raw) {
			return nil, fmt.Errorf("-icon with -override-manifest requires an XML manifest, not binary AXML")
		}
		if !looksLikeXMLManifest(raw) {
			return nil, fmt.Errorf("%s: not an XML AndroidManifest", opts.OverrideManifestPath)
		}
		return injectApplicationIcon(raw), nil
	}
	p := opts.Manifest
	p.IconRef = launcherIconRef
	return []byte(FormatManifestXML(p)), nil
}

func injectApplicationIcon(xml []byte) []byte {
	s := string(xml)
	if strings.Contains(s, "android:icon=") {
		return xml
	}
	const tag = "<application"
	i := strings.Index(s, tag)
	if i < 0 {
		return xml
	}
	insert := `<application android:icon="` + launcherIconRef + `"`
	return []byte(s[:i] + insert + s[i+len(tag):])
}

func readIconPack(apkPath string) (*iconPack, error) {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	pack := &iconPack{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		switch {
		case f.Name == "AndroidManifest.xml":
			pack.Manifest = data
		case f.Name == "resources.arsc":
			pack.ResourcesArsc = data
		case strings.HasPrefix(f.Name, "res/"):
			pack.Res = append(pack.Res, fileEntry{name: f.Name, data: data})
		}
	}
	if pack.Manifest == nil {
		return nil, fmt.Errorf("aapt2 icon output missing AndroidManifest.xml")
	}
	if pack.ResourcesArsc == nil {
		return nil, fmt.Errorf("aapt2 icon output missing resources.arsc")
	}
	if len(pack.Res) == 0 {
		return nil, fmt.Errorf("aapt2 icon output missing res/ entries")
	}
	return pack, nil
}

func isPNG(data []byte) bool {
	return len(data) >= 8 &&
		data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e && data[3] == 0x47 &&
		data[4] == 0x0d && data[5] == 0x0a && data[6] == 0x1a && data[7] == 0x0a
}
