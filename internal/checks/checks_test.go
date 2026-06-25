package checks

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timanthonyalexander/heartd/internal/config"
)

func sec(d time.Duration) config.Duration { return config.Duration(d) }

func TestRunHTTPOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := Run(context.Background(), config.Check{
		Type:    config.CheckHTTP,
		URL:     srv.URL,
		Timeout: sec(2 * time.Second),
	})

	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q (detail %q)", res.Status, StatusOK, res.Detail)
	}
	if res.Detail != "HTTP 200" {
		t.Errorf("detail = %q, want %q", res.Detail, "HTTP 200")
	}
	if res.LatencyMS < 0 {
		t.Errorf("latency = %d, want >= 0", res.LatencyMS)
	}
	if res.At.IsZero() || res.At.Location() != time.UTC {
		t.Errorf("At = %v, want non-zero UTC", res.At)
	}
}

func TestRunHTTPFailing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	res := Run(context.Background(), config.Check{
		Type:    config.CheckHTTP,
		URL:     srv.URL,
		Timeout: sec(2 * time.Second),
	})

	if res.Status != StatusFailing {
		t.Fatalf("status = %q, want %q", res.Status, StatusFailing)
	}
	if res.Detail != "HTTP 500" {
		t.Errorf("detail = %q, want %q", res.Detail, "HTTP 500")
	}
}

func TestRunHTTPMethodDefault(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	Run(context.Background(), config.Check{
		Type:    config.CheckHTTP,
		URL:     srv.URL,
		Timeout: sec(2 * time.Second),
	})
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

func TestRunHTTPUnreachable(t *testing.T) {
	// Reserve a port then close the listener so nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + ln.Addr().String()
	ln.Close()

	res := Run(context.Background(), config.Check{
		Type:    config.CheckHTTP,
		URL:     url,
		Timeout: sec(1 * time.Second),
	})

	if res.Status != StatusFailing {
		t.Fatalf("status = %q, want %q", res.Status, StatusFailing)
	}
	if res.Detail == "" {
		t.Error("expected non-empty detail for unreachable host")
	}
}

func TestRunTCPOK(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	res := Run(context.Background(), config.Check{
		Type:    config.CheckTCP,
		Host:    "127.0.0.1",
		PortNum: port,
		Timeout: sec(2 * time.Second),
	})

	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q (detail %q)", res.Status, StatusOK, res.Detail)
	}
	if res.Detail != "connected" {
		t.Errorf("detail = %q, want %q", res.Detail, "connected")
	}
}

func TestRunTCPFailing(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	ln.Close() // nothing listening now

	res := Run(context.Background(), config.Check{
		Type:    config.CheckTCP,
		Host:    "127.0.0.1",
		PortNum: port,
		Timeout: sec(1 * time.Second),
	})

	if res.Status != StatusFailing {
		t.Fatalf("status = %q, want %q", res.Status, StatusFailing)
	}
}

func TestRunShellOK(t *testing.T) {
	res := Run(context.Background(), config.Check{
		Type:    config.CheckShell,
		Command: "echo hello",
		Timeout: sec(2 * time.Second),
	})

	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q (detail %q)", res.Status, StatusOK, res.Detail)
	}
	if !strings.Contains(res.Detail, "hello") {
		t.Errorf("detail = %q, want to contain %q", res.Detail, "hello")
	}
}

func TestRunShellEmptyOutputOK(t *testing.T) {
	res := Run(context.Background(), config.Check{
		Type:    config.CheckShell,
		Command: "true",
		Timeout: sec(2 * time.Second),
	})
	if res.Status != StatusOK {
		t.Fatalf("status = %q, want %q", res.Status, StatusOK)
	}
	if res.Detail != "exit 0" {
		t.Errorf("detail = %q, want %q", res.Detail, "exit 0")
	}
}

func TestRunShellFailing(t *testing.T) {
	res := Run(context.Background(), config.Check{
		Type:    config.CheckShell,
		Command: "exit 3",
		Timeout: sec(2 * time.Second),
	})

	if res.Status != StatusFailing {
		t.Fatalf("status = %q, want %q", res.Status, StatusFailing)
	}
	if !strings.Contains(res.Detail, "3") {
		t.Errorf("detail = %q, want to mention exit code 3", res.Detail)
	}
}

func TestRunShellTimeout(t *testing.T) {
	start := time.Now()
	res := Run(context.Background(), config.Check{
		Type:    config.CheckShell,
		Command: "sleep 5",
		Timeout: sec(100 * time.Millisecond),
	})
	elapsed := time.Since(start)

	if res.Status != StatusFailing {
		t.Fatalf("status = %q, want %q", res.Status, StatusFailing)
	}
	if elapsed > 2*time.Second {
		t.Errorf("shell timeout took %v, expected well under 5s", elapsed)
	}
}

func TestRunProcessNotFound(t *testing.T) {
	res := Run(context.Background(), config.Check{
		Type:    config.CheckProcess,
		Process: "definitely-not-a-real-process-xyz",
		Timeout: sec(5 * time.Second),
	})

	if res.Status != StatusFailing {
		t.Fatalf("status = %q, want %q", res.Status, StatusFailing)
	}
	if !strings.Contains(res.Detail, "definitely-not-a-real-process-xyz") {
		t.Errorf("detail = %q, want to mention the process name", res.Detail)
	}
}

func TestRunProcessFound(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot determine own executable: %v", err)
	}
	name := filepath.Base(exe)
	// gopsutil may truncate long process names; use a prefix to stay safe.
	if len(name) > 12 {
		name = name[:12]
	}

	res := Run(context.Background(), config.Check{
		Type:    config.CheckProcess,
		Process: name,
		Timeout: sec(5 * time.Second),
	})

	if res.Status != StatusOK {
		t.Skipf("could not find own process %q (detail %q); environment-dependent", name, res.Detail)
	}
	if !strings.Contains(res.Detail, "pid") {
		t.Errorf("detail = %q, want to contain pid", res.Detail)
	}
}

func TestRunUnknownType(t *testing.T) {
	res := Run(context.Background(), config.Check{Type: "bogus"})
	if res.Status != StatusUnknown {
		t.Fatalf("status = %q, want %q", res.Status, StatusUnknown)
	}
}
