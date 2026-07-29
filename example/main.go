package main

import (
	"bytes"
	"log"

	"github.com/ktago336/gofront"
)

type API struct {
	backend *gofront.App
}

type Node struct {
	Name     string  `json:"name"`
	Children []*Node `json:"children,omitempty"`
}

type Status struct {
	OK    bool     `json:"ok"`
	Count int      `json:"count"`
	Tags  []string `json:"tags"`
}

func (a *API) Hello(name string) string {
	return "Hello, " + name + "! — greetings from Go running on Android."
}

func (a *API) Add(x, y float64) float64 {
	if err := a.backend.AndroidAPI.Notify("ANDROID APP", "Calculation called!"); err != nil {
		log.Printf("notify: %v", err)
	}
	return x + y
}

func (a *API) Upper(data []byte) []byte {
	return bytes.ToUpper(data)
}

func (a *API) Status() Status {
	return Status{OK: true, Count: 3, Tags: []string{"go", "android", "webview"}}
}

func (a *API) Tree() *Node {
	return &Node{Name: "root", Children: []*Node{{Name: "a"}, {Name: "b"}}}
}

func main() {
	app := gofront.New()
	app.Bind("api", &API{backend: app})

	go func() {
		if err := app.Run(); err != nil {
			log.Fatal(err)
		}
	}()

	select {}
}
