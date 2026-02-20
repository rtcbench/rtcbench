package cztest_client

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	minPollSec = 5
	maxPollSec = 10
)

var (
	httpClient = &http.Client{Timeout: 10 * time.Second}
	curUUID    = ""
	baseURL    string

	mu      sync.Mutex
	running *exec.Cmd
)

type ClientArgs struct {
	ServerIP   string
	ServerPort int
}

func StartTestClient(args *ClientArgs) {
	baseURL = fmt.Sprintf("http://%s:%d", args.ServerIP, args.ServerPort)
	log.Printf("Will poll %s randomly between %d and %d seconds after initial jitter...", baseURL, minPollSec, maxPollSec)

	time.Sleep(time.Duration(rand.Intn((maxPollSec-minPollSec)*1000)) * time.Millisecond)

	for {
		pollForWork(baseURL)
		time.Sleep(time.Duration(minPollSec*1000+rand.Intn((maxPollSec-minPollSec)*1000)) * time.Millisecond)
	}
}

func pollForWork(url string) {
	resp, err := httpClient.Get(url + "/work")
	if err != nil {
		log.Printf("pollForWork: %v", err)
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		onStop()
		return

	case http.StatusOK:
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("pollForWork: read: %v", err)
			return
		}
		u := strings.TrimSpace(string(b))
		if u != "" && u != curUUID {
			log.Printf("New work UUID: %s", u)
			curUUID = u
			onStart()
		}
		return

	default:
		log.Printf("pollForWork: unexpected status %d", resp.StatusCode)
		return
	}
}

func onStop() {
	mu.Lock()
	cmd := running
	running = nil
	mu.Unlock()

	if cmd == nil {
		return
	}

	if err := stopCmd(cmd, 10*time.Second); err != nil {
		log.Printf("onStop: %v", err)
	}
}

func onStart() {
	uuid := curUUID
	if uuid == "" || baseURL == "" {
		return
	}

	if err := startPayload(uuid, baseURL); err != nil {
		log.Printf("onStart: %v", err)
	}
}

func startPayload(uuid, baseURL string) error {
	dir := filepath.Join("cztest-downloads", uuid)
	cfgPath := filepath.Join(dir, "config.yml")
	binPath := filepath.Join(dir, "cztest-payload")

	if err := downloadToFile(baseURL+"/config", cfgPath, 0o644); err != nil {
		return fmt.Errorf("config download failed: %w", err)
	}

	binURL := fmt.Sprintf("%s/binary?os=%s&arch=%s", baseURL, runtime.GOOS, runtime.GOARCH)
	if err := downloadToFile(binURL, binPath, 0o755); err != nil {
		return fmt.Errorf("binary download failed: %w", err)
	}

	cmd := exec.Command("./cztest-payload", "config.yml")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	mu.Lock()
	old := running
	running = cmd
	mu.Unlock()

	if old != nil {
		_ = stopCmd(old, 10*time.Second)
	}

	go func(c *exec.Cmd) {
		_ = c.Wait()
		mu.Lock()
		if running == c {
			running = nil
		}
		mu.Unlock()
	}(cmd)

	return nil
}

func downloadToFile(url, path string, mode os.FileMode) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}

	return os.Rename(tmp, path)
}

func stopCmd(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			return errors.New("force kill timed out")
		}
	}
}
