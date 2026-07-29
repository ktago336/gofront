// Command gofront builds a Go + web project into an Android .apk.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ktago336/gofront/internal/apk"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		if err := cmdBuild(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "gofront: "+err.Error())
			os.Exit(1)
		}
	case "init-manifest":
		if err := cmdInitManifest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "gofront: "+err.Error())
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "gofront: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `gofront - build a Go backend + JS frontend into one Android .apk

Usage:
  gofront build [flags] [package-dir]
  gofront build -h
  gofront init-manifest [flags] [dir]
  gofront init-manifest -h

`)
	printBuildFlags(os.Stderr)
	fmt.Fprintln(os.Stderr)
	printInitManifestFlags(os.Stderr)
}

func printBuildFlags(w *os.File) {
	fmt.Fprint(w, `build flags:
  -o string             output apk path (default "app.apk")
  -frontend string      frontend dir to bundle (default "<package-dir>/frontend")
  -abi string           target Android ABI (default "arm64-v8a")
  -package string       app package id (default "com.gofront.app")
  -label string         app display name (default "GoFront")
  -version-code int     android versionCode (default 1)
  -version-name string  android versionName (default "1.0")
  -min-sdk int          minSdkVersion (default 21)
  -target-sdk int       targetSdkVersion (default 28)
  -icon string          PNG path for the launcher icon
  -override-manifest    path to AndroidManifest (binary AXML, or XML via aapt2)
  -install              adb install -r after building
  -run                  adb launch the app after installing
`)
}

func printInitManifestFlags(w *os.File) {
	fmt.Fprint(w, `init-manifest flags:
  -o string             output path (default "<dir>/AndroidManifest.xml")
  -f                    overwrite if the file already exists
`)
}

func cmdInitManifest(args []string) error {
	fs := flag.NewFlagSet("init-manifest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: gofront init-manifest [flags] [dir]

Write the default AndroidManifest.xml (same as gofront build without
-override-manifest) so you can edit it and pass -override-manifest.

`)
		printInitManifestFlags(os.Stderr)
	}
	out := fs.String("o", "", "output path (default <dir>/AndroidManifest.xml)")
	force := fs.Bool("f", false, "overwrite if the file already exists")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	dir := "."
	if len(positional) > 0 {
		dir = positional[0]
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	path := *out
	if path == "" {
		path = filepath.Join(dir, "AndroidManifest.xml")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}

	if !*force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use -f to overwrite)", path)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	xml := apk.FormatManifestXML(apk.DefaultManifestParams())
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		return err
	}
	fmt.Printf("==> wrote %s\n", path)
	return nil
}

func cmdBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: gofront build [flags] [package-dir]\n\n")
		printBuildFlags(os.Stderr)
	}
	out := fs.String("o", "app.apk", "output apk path")
	frontend := fs.String("frontend", "", "frontend dir to bundle (default <pkg>/frontend)")
	abi := fs.String("abi", "arm64-v8a", "target Android ABI")
	pkg := fs.String("package", "com.gofront.app", "app package id")
	label := fs.String("label", "GoFront", "app display name")
	versionCode := fs.Int("version-code", 1, "android versionCode")
	versionName := fs.String("version-name", "1.0", "android versionName")
	minSDK := fs.Int("min-sdk", 21, "minSdkVersion")
	targetSDK := fs.Int("target-sdk", 28, "targetSdkVersion")
	icon := fs.String("icon", "", "PNG path for the launcher icon")
	overrideManifest := fs.String("override-manifest", "", "path to AndroidManifest (binary AXML or XML)")
	install := fs.Bool("install", false, "adb install -r after building")
	run := fs.Bool("run", false, "adb launch the app after installing")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	pkgDir := "."
	if len(positional) > 0 {
		pkgDir = positional[0]
	}
	pkgDir, err = filepath.Abs(pkgDir)
	if err != nil {
		return err
	}
	if *frontend == "" {
		*frontend = filepath.Join(pkgDir, "frontend")
	}

	goarch, err := abiToGoarch(*abi)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "gofront-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	fmt.Println("==> generating JS bindings")
	hostBin := filepath.Join(work, "host")
	if err := goBuild(pkgDir, hostBin, "", ""); err != nil {
		return fmt.Errorf("host build: %w", err)
	}
	if err := runCmd(hostBin, "-gofront-generate", *frontend); err != nil {
		return fmt.Errorf("generate bindings: %w", err)
	}

	fmt.Printf("==> compiling Go server for android/%s\n", goarch)
	serverBin := filepath.Join(work, "libserver.so")
	if err := goBuild(pkgDir, serverBin, goarch, "-s -w"); err != nil {
		return fmt.Errorf("cross build: %w", err)
	}

	fmt.Println("==> packaging apk")
	opts := apk.Options{
		ServerBinary: serverBin,
		ABI:          *abi,
		FrontendDir:  *frontend,
		Output:       *out,
		IconPath:     *icon,
		Manifest: apk.ManifestParams{
			Package:     *pkg,
			Label:       *label,
			VersionCode: *versionCode,
			VersionName: *versionName,
			MinSDK:      *minSDK,
			TargetSDK:   *targetSDK,
		},
	}
	if *overrideManifest != "" {
		fmt.Printf("==> using manifest override %s\n", *overrideManifest)
		if *icon != "" {
			// aapt2 must see XML so it can link @mipmap/ic_launcher into the same package.
			opts.OverrideManifestPath = *overrideManifest
		} else {
			data, err := apk.LoadManifest(*overrideManifest)
			if err != nil {
				return fmt.Errorf("override-manifest: %w", err)
			}
			opts.ManifestXML = data
		}
	}
	if err := apk.Build(opts); err != nil {
		return err
	}
	fmt.Printf("==> wrote %s\n", *out)

	if *install || *run {
		if err := runCmd("adb", "install", "-r", "--bypass-low-target-sdk-block", *out); err != nil {
			return fmt.Errorf("adb install: %w", err)
		}
		fmt.Println("==> installed")
	}
	if *run {
		component := *pkg + "/com.gofront.app.MainActivity"
		if err := runCmd("adb", "shell", "am", "start", "-n", component); err != nil {
			return fmt.Errorf("adb start: %w", err)
		}
		fmt.Println("==> launched")
	}
	return nil
}

// parseInterspersed accepts flags before or after positional args.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	return positional, nil
}

func abiToGoarch(abi string) (string, error) {
	switch abi {
	case "arm64-v8a":
		return "arm64", nil
	case "armeabi-v7a":
		return "arm", nil
	case "x86_64":
		return "amd64", nil
	case "x86":
		return "386", nil
	default:
		return "", fmt.Errorf("unsupported abi %q", abi)
	}
}

// goBuild builds dir into out. Empty goarch = host; otherwise static linux/<arch>.
func goBuild(dir, out, goarch, ldflags string) error {
	args := []string{"build", "-o", out}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if goarch != "" {
		cmd.Env = append(cmd.Env,
			"GOOS=linux",
			"GOARCH="+goarch,
			"CGO_ENABLED=0",
		)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
