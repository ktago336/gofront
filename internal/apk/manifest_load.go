package apk

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	androidAPI        = 28
	androidJarURL     = "https://raw.githubusercontent.com/Sable/android-platforms/master/android-28/android.jar"
	buildToolsLinux   = "https://dl.google.com/android/repository/build-tools_r34-linux.zip"
	buildToolsDarwin  = "https://dl.google.com/android/repository/build-tools_r34-macosx.zip"
	buildToolsWindows = "https://dl.google.com/android/repository/build-tools_r34-windows.zip"
)

// LoadManifest reads path as either a compiled binary AXML or a text XML
// AndroidManifest. Binary needs no extra tools. XML is compiled with aapt2
// (native binary; JDK not required), downloaded into the user cache on first use.
func LoadManifest(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isBinaryAXML(data) {
		return data, nil
	}
	if !looksLikeXMLManifest(data) {
		return nil, fmt.Errorf("%s: not a binary AXML or XML AndroidManifest", path)
	}
	return compileXMLManifest(path)
}

func isBinaryAXML(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x03 && data[1] == 0x00
}

func looksLikeXMLManifest(data []byte) bool {
	s := strings.TrimSpace(string(data))
	return strings.HasPrefix(s, "<?xml") || strings.Contains(s, "<manifest")
}

func compileXMLManifest(xmlPath string) ([]byte, error) {
	abs, err := filepath.Abs(xmlPath)
	if err != nil {
		return nil, err
	}
	jar, aapt2, err := ensureAAPT2()
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "gofront-manifest-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	outAPK := filepath.Join(work, "m.apk")
	cmd := exec.Command(aapt2, "link",
		"-o", outAPK,
		"-I", jar,
		"--manifest", abs,
		"--min-sdk-version", "21",
		"--target-sdk-version", fmt.Sprintf("%d", androidAPI),
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("aapt2 link: %w", err)
	}

	zr, err := zip.OpenReader(outAPK)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "AndroidManifest.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		return b, err
	}
	return nil, fmt.Errorf("aapt2 output missing AndroidManifest.xml")
}

func ensureAAPT2() (androidJar, aapt2 string, err error) {
	cache, err := cacheDir()
	if err != nil {
		return "", "", err
	}
	androidJar = filepath.Join(cache, "android.jar")
	if _, err := os.Stat(androidJar); err != nil {
		fmt.Println("==> downloading android.jar (for manifest compile)")
		if err := downloadFile(androidJarURL, androidJar); err != nil {
			return "", "", fmt.Errorf("android.jar: %w", err)
		}
	}

	btDir := filepath.Join(cache, "bt", "android-14")
	aapt2 = filepath.Join(btDir, "aapt2")
	if runtime.GOOS == "windows" {
		aapt2 += ".exe"
	}
	if _, err := os.Stat(aapt2); err != nil {
		url, err := buildToolsURL()
		if err != nil {
			return "", "", err
		}
		fmt.Println("==> downloading Android build-tools (aapt2)")
		zipPath := filepath.Join(cache, "bt.zip")
		if err := downloadFile(url, zipPath); err != nil {
			return "", "", err
		}
		_ = os.RemoveAll(filepath.Join(cache, "bt"))
		if err := unzipTo(zipPath, filepath.Join(cache, "bt")); err != nil {
			return "", "", err
		}
		if _, err := os.Stat(aapt2); err != nil {
			return "", "", fmt.Errorf("aapt2 not found after extract at %s", aapt2)
		}
		_ = os.Chmod(aapt2, 0o755)
	}
	return androidJar, aapt2, nil
}

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "gofront", "android")
	return dir, os.MkdirAll(dir, 0o755)
}

func buildToolsURL() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return buildToolsLinux, nil
	case "darwin":
		return buildToolsDarwin, nil
	case "windows":
		return buildToolsWindows, nil
	default:
		return "", fmt.Errorf("unsupported OS %q for aapt2 download", runtime.GOOS)
	}
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %s", url, resp.Status)
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	cerr := f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if cerr != nil {
		os.Remove(tmp)
		return cerr
	}
	return os.Rename(tmp, dest)
}

func unzipTo(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("zip slip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}