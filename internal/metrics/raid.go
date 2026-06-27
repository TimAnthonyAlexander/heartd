package metrics

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// RaidArray is the state of one Linux software-RAID (mdadm) array, parsed from
// /proc/mdstat. It is informational only — never an alert source. State is one
// of: clean, degraded, rebuilding, failed (see deriveRaidState).
type RaidArray struct {
	Name          string  // e.g. "md2"
	Level         string  // e.g. "raid1"
	State         string  // clean | degraded | rebuilding | failed
	TotalDevices  int     // T in the [T/A] count
	ActiveDevices int     // A in the [T/A] count
	ResyncPercent float64 // rebuild/resync progress, 0 when not rebuilding
	Detail        string  // progress line (percent + ETA) when rebuilding, else ""
}

// mdstatPath returns the file to parse, allowing an override for tests. Read at
// call time (not cached) so a test can point it at a fixture per call.
func mdstatPath() string {
	if p := os.Getenv("HEARTD_MDSTAT_PATH"); p != "" {
		return p
	}
	return "/proc/mdstat"
}

var (
	// mdHeaderRe matches a block header line: "md2 : active raid1 sdb3[0] sda3[1]".
	mdHeaderRe = regexp.MustCompile(`^(md\w+)\s*:\s*(.*)$`)
	// mdMemberRe matches a member device token, e.g. "sda3[1]" or "sda3[1](F)".
	mdMemberRe = regexp.MustCompile(`^[\w.-]+\[\d+\]`)
	// mdCountRe matches the [Total/Active] device count, e.g. "[2/2]".
	mdCountRe = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
	// mdStateRe matches the per-member up/down string, e.g. "[UU]" or "[U_]".
	mdStateRe = regexp.MustCompile(`\[([U_]+)\]`)
	// mdProgressRe matches an in-flight rebuild/resync progress line carrying a
	// numeric percentage. PENDING (a queued-but-idle resync) has no percent, so it
	// deliberately does NOT match — that is the false-positive guard.
	mdProgressRe = regexp.MustCompile(`(?:recovery|resync|reshape|check)\s*=\s*([0-9.]+)%[^\n]*`)
)

// ReadRaidArrays parses /proc/mdstat (or HEARTD_MDSTAT_PATH) into per-array
// state. A missing file (e.g. macOS, or a host without mdadm) is NOT an error —
// it returns an empty slice, since absence of software RAID is normal.
func ReadRaidArrays() ([]RaidArray, error) {
	data, err := os.ReadFile(mdstatPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("metrics: read mdstat: %w", err)
	}
	return parseMdstat(string(data)), nil
}

// parseMdstat splits the file into md blocks and builds a RaidArray from each.
// A block is a header line ("mdN : ...") plus its indented continuation lines,
// terminated by a blank line, a non-indented line, or the next header.
func parseMdstat(content string) []RaidArray {
	var out []RaidArray

	var (
		name   string
		header string
		block  strings.Builder
		open   bool
	)
	flush := func() {
		if !open {
			return
		}
		out = append(out, buildRaidArray(name, header, block.String()))
		open = false
		block.Reset()
	}

	for _, line := range strings.Split(content, "\n") {
		if m := mdHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			name, header, open = m[1], m[2], true
			block.WriteString(m[2])
			block.WriteByte('\n')
			continue
		}
		if !open {
			continue
		}
		// A blank line or a non-indented line (e.g. "unused devices:") ends the
		// current array's block.
		if strings.TrimSpace(line) == "" || (line != "" && line[0] != ' ' && line[0] != '\t') {
			flush()
			continue
		}
		block.WriteString(line)
		block.WriteByte('\n')
	}
	flush()
	return out
}

// buildRaidArray assembles one array's state from its header and block text.
func buildRaidArray(name, header, block string) RaidArray {
	a := RaidArray{Name: name}

	fields := strings.Fields(header)
	activity := ""
	if len(fields) > 0 {
		activity = fields[0]
	}
	// Level is the first token after the activity word that is neither a
	// parenthesised flag ("(auto-read-only)") nor a member device ("sda3[1]").
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "(") {
			continue
		}
		if mdMemberRe.MatchString(f) {
			break
		}
		a.Level = f
		break
	}

	if m := mdCountRe.FindStringSubmatch(block); m != nil {
		a.TotalDevices, _ = strconv.Atoi(m[1])
		a.ActiveDevices, _ = strconv.Atoi(m[2])
	}

	stateStr := ""
	if m := mdStateRe.FindStringSubmatch(block); m != nil {
		stateStr = m[1]
	}

	faulty := strings.Contains(block, "(F)")

	hasProgress := false
	if m := mdProgressRe.FindStringSubmatch(block); m != nil {
		hasProgress = true
		a.ResyncPercent, _ = strconv.ParseFloat(m[1], 64)
		a.Detail = strings.TrimSpace(m[0])
	}

	a.State = deriveRaidState(activity, stateStr, a.TotalDevices, a.ActiveDevices, faulty, hasProgress)
	return a
}

// deriveRaidState classifies an array EXACTLY per the spec's rules. Priority:
// failed > rebuilding > degraded > clean.
//
//   - failed     — the array is not "active", or every member is down.
//   - rebuilding — a numeric recovery/resync progress line is present.
//   - degraded   — a member is down ('_'), active < total, or a member is faulty.
//   - clean      — total == active and the state string is all 'U'.
//
// The PENDING guard lives in mdProgressRe (PENDING carries no percent), so an
// idle "[UU] ... resync=PENDING" array has no progress, no '_', and all members
// active → it correctly falls through to clean.
func deriveRaidState(activity, stateStr string, total, active int, faulty, hasProgress bool) string {
	allDown := stateStr != "" && !strings.ContainsRune(stateStr, 'U')
	if activity != "active" || allDown {
		return "failed"
	}
	if hasProgress {
		return "rebuilding"
	}
	if strings.ContainsRune(stateStr, '_') || (total > 0 && active < total) || faulty {
		return "degraded"
	}
	return "clean"
}
