// Package apk packs and signs an Android APK in pure Go (no Android SDK).
package apk

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ktago336/gofront/internal/assets"
)

type Options struct {
	ServerBinary string // path to the linux/<abi> Go server binary
	ABI          string // e.g. arm64-v8a
	FrontendDir  string // bundled under assets/frontend/
	Output       string
	Manifest     ManifestParams
	// ManifestXML, if non-nil, replaces the generated AndroidManifest.xml
	// (compiled binary AXML bytes).
	ManifestXML []byte
}

type fileEntry struct {
	name  string
	data  []byte
	store bool // uncompressed (needed for resources.arsc)
}

func Build(opts Options) error {
	if opts.ABI == "" {
		opts.ABI = "arm64-v8a"
	}

	server, err := os.ReadFile(opts.ServerBinary)
	if err != nil {
		return fmt.Errorf("read server binary: %w", err)
	}

	manifest := opts.ManifestXML
	if manifest == nil {
		manifest = EncodeManifest(opts.Manifest)
	}

	entries := []fileEntry{
		{name: "AndroidManifest.xml", data: manifest},
		{name: "resources.arsc", data: assets.ResourcesArsc, store: true},
		{name: "classes.dex", data: assets.ClassesDex},
		{name: "lib/" + opts.ABI + "/libserver.so", data: server},
	}

	if opts.FrontendDir != "" {
		fe, err := collectFrontend(opts.FrontendDir)
		if err != nil {
			return err
		}
		entries = append(entries, fe...)
	}

	unsigned, err := zipBytes(entries)
	if err != nil {
		return err
	}
	signed, err := signAPK(unsigned)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	if dir := filepath.Dir(opts.Output); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(opts.Output, signed, 0o644)
}

func collectFrontend(dir string) ([]fileEntry, error) {
	var out []fileEntry
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := "assets/frontend/" + filepath.ToSlash(rel)
		out = append(out, fileEntry{name: name, data: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect frontend: %w", err)
	}
	return out, nil
}

func zipBytes(entries []fileEntry) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		method := zip.Deflate
		if e.store {
			method = zip.Store
		}
		hdr := &zip.FileHeader{Name: e.name, Method: method}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(e.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
