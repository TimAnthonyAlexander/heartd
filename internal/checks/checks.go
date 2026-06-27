// Package checks executes configured service health checks (HTTP, TCP, process,
// and shell) and reports their outcome as a Result. It is the runtime
// counterpart to config.Check: given a single check definition it performs the
// underlying probe, bounded by the check's Timeout, and never returns an error —
// every failure mode is encoded as a Result with Status == StatusFailing and a
// descriptive Detail.
package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"github.com/timanthonyalexander/heartd/internal/config"
	"github.com/timanthonyalexander/heartd/internal/version"
)

// Status is the outcome of a check.
type Status string

const (
	// StatusOK indicates the check passed.
	StatusOK Status = "ok"
	// StatusFailing indicates the check ran but did not pass.
	StatusFailing Status = "failing"
	// StatusUnknown indicates the check could not be evaluated (e.g. unknown type).
	StatusUnknown Status = "unknown"
)

// maxDetailLen caps the length of captured output stored in Detail so a noisy
// command cannot bloat results.
const maxDetailLen = 500

// Result is the outcome of running a single check.
type Result struct {
	Status    Status    // ok or failing (unknown only for unrecognised types)
	Detail    string    // human-readable, e.g. "HTTP 200", "connected", "exit 0", or the failure reason
	LatencyMS int64     // measured latency in ms where meaningful (http, tcp); 0 otherwise
	At        time.Time // when the check ran (UTC)
}

// Run executes a single check honoring c.Timeout, and returns a Result. It NEVER
// returns an error — every failure (timeout, bad status, connection refused,
// non-zero exit, process not found) is encoded as Status=StatusFailing with a
// descriptive Detail. The passed ctx bounds the whole operation; Run also applies
// c.Timeout on top.
func Run(ctx context.Context, c config.Check) Result {
	at := time.Now().UTC()

	if c.Timeout.Std() > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout.Std())
		defer cancel()
	}

	var res Result
	switch c.Type {
	case config.CheckHTTP:
		res = runHTTP(ctx, c)
	case config.CheckTCP:
		res = runTCP(ctx, c)
	case config.CheckProcess:
		res = runProcess(ctx, c)
	case config.CheckShell:
		res = runShell(ctx, c)
	default:
		res = Result{Status: StatusUnknown, Detail: fmt.Sprintf("unknown check type %q", c.Type)}
	}

	res.At = at
	return res
}

// runHTTP performs an HTTP request and classifies the response by status code.
func runHTTP(ctx context.Context, c config.Check) Result {
	method := c.Method
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequestWithContext(ctx, method, c.URL, nil)
	if err != nil {
		return Result{Status: StatusFailing, Detail: fmt.Sprintf("invalid request: %v", err)}
	}
	req.Header.Set("User-Agent", httpUserAgent(c.UserAgent))

	client := &http.Client{Timeout: c.Timeout.Std()}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return Result{Status: StatusFailing, Detail: err.Error(), LatencyMS: latency.Milliseconds()}
	}
	defer resp.Body.Close()

	ok, expected := httpStatusAccepted(resp.StatusCode, c.AcceptAny, c.AcceptedStatuses)
	status := StatusFailing
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if ok {
		status = StatusOK
	} else {
		detail = fmt.Sprintf("HTTP %d (expected %s)", resp.StatusCode, expected)
	}
	return Result{
		Status:    status,
		Detail:    detail,
		LatencyMS: latency.Milliseconds(),
	}
}

// httpUserAgent returns the per-check override when set, else the default
// recognizable heartd health-check User-Agent so far ends can allow-list us.
func httpUserAgent(override string) string {
	if override != "" {
		return override
	}
	return "heartd/" + version.Version + " (health-check)"
}

// httpStatusAccepted decides whether code counts as healthy and, when not,
// returns a human-readable description of what was expected (for the failure
// detail). Precedence: acceptAny > explicit list > 2xx default.
func httpStatusAccepted(code int, acceptAny bool, accepted []int) (ok bool, expected string) {
	if acceptAny {
		return true, "any response"
	}
	if len(accepted) > 0 {
		for _, c := range accepted {
			if c == code {
				return true, ""
			}
		}
		return false, "one of " + joinCodes(accepted)
	}
	return code >= 200 && code <= 299, "2xx"
}

// joinCodes renders a status-code list as "200, 401, 403".
func joinCodes(codes []int) string {
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = strconv.Itoa(c)
	}
	return strings.Join(parts, ", ")
}

// runTCP attempts a TCP connection to host:port.
func runTCP(ctx context.Context, c config.Check) Result {
	addr := net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.PortNum))

	dialer := net.Dialer{Timeout: c.Timeout.Std()}

	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	latency := time.Since(start)
	if err != nil {
		return Result{Status: StatusFailing, Detail: err.Error(), LatencyMS: latency.Milliseconds()}
	}
	defer conn.Close()

	return Result{Status: StatusOK, Detail: "connected", LatencyMS: latency.Milliseconds()}
}

// runProcess checks whether any running process matches c.Process by executable
// name (case-insensitive, substring match).
func runProcess(ctx context.Context, c config.Check) Result {
	target := strings.ToLower(c.Process)

	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return Result{Status: StatusFailing, Detail: fmt.Sprintf("cannot list processes: %v", err)}
	}

	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(name), target) {
			return Result{Status: StatusOK, Detail: fmt.Sprintf("running (pid %d)", p.Pid)}
		}
	}

	return Result{Status: StatusFailing, Detail: fmt.Sprintf("no process matching %q", c.Process)}
}

// runShell runs c.Command via `sh -c` and classifies the result by exit code.
func runShell(ctx context.Context, c config.Check) Result {
	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())

	if err == nil {
		detail := truncate(out, maxDetailLen)
		if detail == "" {
			detail = "exit 0"
		}
		return Result{Status: StatusOK, Detail: detail}
	}

	// Timeout / deadline exceeded.
	if ctx.Err() != nil {
		return Result{Status: StatusFailing, Detail: fmt.Sprintf("timed out: %v", ctx.Err())}
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := errOut
		if msg == "" {
			msg = out
		}
		detail := fmt.Sprintf("exit %d", exitErr.ExitCode())
		if msg != "" {
			detail = fmt.Sprintf("exit %d: %s", exitErr.ExitCode(), truncate(msg, maxDetailLen))
		}
		return Result{Status: StatusFailing, Detail: detail}
	}

	return Result{Status: StatusFailing, Detail: truncate(err.Error(), maxDetailLen)}
}

// truncate shortens s to at most n characters, appending an ellipsis marker when
// content was removed.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
