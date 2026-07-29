// Package gofront serves a frontend directory over HTTP and exposes bound Go
// methods to JavaScript (see App.Bind). Build an APK with the gofront CLI.
package gofront

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
)

// App is a local HTTP server plus a set of Go objects callable from JS.
type App struct {
	Addr        string // default 127.0.0.1:8080; override with -addr
	FrontendDir string // default "frontend"; override with -frontend
	// AndroidAPI calls Android host APIs (notifications, …) from Go.
	AndroidAPI *AndroidAPI
	binder     *Binder
}

func New() *App {
	return &App{
		Addr:        "127.0.0.1:8080",
		FrontendDir: "frontend",
		AndroidAPI:  newAndroidAPI(),
		binder:      newBinder(),
	}
}

// Bind registers obj under name. Exported methods become
// window.gofront.<name>.<Method>(...).
func (a *App) Bind(name string, obj interface{}) *App {
	a.binder.bind(name, reflect.ValueOf(obj))
	return a
}

// Run starts the server, or with -gofront-generate <dir> writes bindings and exits.
func (a *App) Run() error {
	fs := flag.NewFlagSet("gofront", flag.ContinueOnError)
	addr := fs.String("addr", a.Addr, "address to listen on")
	frontend := fs.String("frontend", a.FrontendDir, "frontend directory to serve")
	genDir := fs.String("gofront-generate", "", "generate JS bindings into this dir and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	a.Addr = *addr
	a.FrontendDir = *frontend

	if *genDir != "" {
		if err := a.GenerateBindings(*genDir); err != nil {
			fmt.Fprintln(os.Stderr, "gofront: "+err.Error())
			os.Exit(1)
		}
		// Build runs this binary only to emit bindings; exit so user code
		// after Run (or a blocking main) cannot hang gofront build.
		os.Exit(0)
	}
	return a.serve()
}

// GenerateBindings writes gofront.js and gofront.d.ts into dir.
func (a *App) GenerateBindings(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	jsPath := filepath.Join(dir, "gofront.js")
	if err := os.WriteFile(jsPath, a.binder.generateJS(), 0o644); err != nil {
		return err
	}
	dtsPath := filepath.Join(dir, "gofront.d.ts")
	if err := os.WriteFile(dtsPath, a.binder.generateTS(), 0o644); err != nil {
		return err
	}
	log.Printf("gofront: wrote bindings to %s and %s", jsPath, dtsPath)
	return nil
}

func (a *App) serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/gofront/call", a.binder.handleCall)
	mux.HandleFunc("/gofront.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(a.binder.generateJS())
	})

	fileServer := http.FileServer(http.Dir(a.FrontendDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			index := filepath.Join(a.FrontendDir, "index.html")
			if _, err := os.Stat(index); err != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write([]byte(defaultPage))
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})

	log.Printf("gofront: serving %q on http://%s", a.FrontendDir, a.Addr)
	return http.ListenAndServe(a.Addr, mux)
}

const defaultPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>GoFront</title></head>
<body style="font-family:sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0">
<h1>Hello from Go running inside Android</h1>
</body></html>`
