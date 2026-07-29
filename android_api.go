package gofront

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultAndroidAPIAddr = "127.0.0.1:8081"
	androidAPIEnv         = "GOFRONT_ANDROID_ADDR"
	androidAPIPath        = "/gofront/android"
)

// AndroidAPI calls into the Android host process (MainActivity bridge).
// Use as app.AndroidAPI.Notify(...). Only works inside a gofront APK.
type AndroidAPI struct {
	// Addr is host:port of the Android bridge. Empty → env GOFRONT_ANDROID_ADDR
	// or 127.0.0.1:8081.
	Addr   string
	client *http.Client
}

func newAndroidAPI() *AndroidAPI {
	return &AndroidAPI{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (a *AndroidAPI) addr() string {
	if a.Addr != "" {
		return a.Addr
	}
	if v := os.Getenv(androidAPIEnv); v != "" {
		return v
	}
	return defaultAndroidAPIAddr
}

type androidCallRequest struct {
	Method string        `json:"method"`
	Args   []interface{} `json:"args"`
}

type androidCallResponse struct {
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}

// Notify shows a status-bar notification on the device.
func (a *AndroidAPI) Notify(title, text string) error {
	return a.invoke("Notify", title, text)
}

func (a *AndroidAPI) invoke(method string, args ...interface{}) error {
	if a.client == nil {
		a.client = &http.Client{Timeout: 5 * time.Second}
	}
	body, err := json.Marshal(androidCallRequest{Method: method, Args: args})
	if err != nil {
		return err
	}
	url := "http://" + a.addr() + androidAPIPath
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("android api %s: %w (not running on device?)", method, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var out androidCallResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Errorf("android api %s: bad response: %w", method, err)
	}
	if out.Error != "" {
		return fmt.Errorf("android api %s: %s", method, out.Error)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("android api %s: HTTP %s", method, resp.Status)
	}
	return nil
}
