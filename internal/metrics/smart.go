package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// SmartTempWarnCeilingC is the soft temperature ceiling above which a disk rolls
// up to "warn". SATA spinning disks above ~55°C are running hot.
const SmartTempWarnCeilingC = 55

// SmartDisk is one drive's SMART health, read from a root-written JSON file
// (heartd runs unprivileged and never invokes smartctl itself). Informational
// only — never an alert source.
type SmartDisk struct {
	Device          string
	Model           string
	Serial          string
	Health          string // overall self-assessment, e.g. "PASSED" / "FAILED"
	Reallocated     uint64
	Pending         uint64
	Uncorrectable   uint64
	CRCErrors       uint64
	TempC           int
	PowerOnHours    uint64
	PowerCycleCount uint64
}

// SmartReport is the parsed SMART file. Present is false when the file is simply
// absent (SMART not collected on this host) or unparsable — both are non-errors:
// SMART is an optional, independent data source. SourceAt is when the file was
// generated (its generated_at field, else the file mtime), used for staleness.
type SmartReport struct {
	Disks    []SmartDisk
	SourceAt time.Time
	Present  bool
}

// smartFile mirrors the on-disk JSON schema (model 1 — see docs/DISK_HEALTH_CARD.md).
type smartFile struct {
	GeneratedAt string          `json:"generated_at"`
	Disks       []smartFileDisk `json:"disks"`
}

type smartFileDisk struct {
	Device          string `json:"device"`
	Model           string `json:"model"`
	Serial          string `json:"serial"`
	Health          string `json:"health"`
	Reallocated     uint64 `json:"reallocated"`
	Pending         uint64 `json:"pending"`
	Uncorrectable   uint64 `json:"uncorrectable"`
	CRCErrors       uint64 `json:"crc_errors"`
	TempC           int    `json:"temp_c"`
	PowerOnHours    uint64 `json:"power_on_hours"`
	PowerCycleCount uint64 `json:"power_cycle_count"`
}

// smartParseWarnOnce ensures a malformed SMART file is logged only once rather
// than every sampling cycle.
var smartParseWarnOnce sync.Once

// smartFilePath returns the file to read, allowing an override for tests. Read
// at call time so a test can point it at a fixture per call.
func smartFilePath() string {
	if p := os.Getenv("HEARTD_SMART_FILE"); p != "" {
		return p
	}
	return "/var/lib/diskhealth/smart.json"
}

// ReadSmart reads the external SMART JSON file. A missing file yields
// Present=false with no error (SMART simply isn't collected here). A malformed
// file is logged once and also yields Present=false rather than failing the
// whole metrics sample.
func ReadSmart() (SmartReport, error) {
	path := smartFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SmartReport{Present: false}, nil
		}
		return SmartReport{}, fmt.Errorf("metrics: read smart file: %w", err)
	}

	var raw smartFile
	if err := json.Unmarshal(data, &raw); err != nil {
		smartParseWarnOnce.Do(func() {
			log.Printf("metrics: smart file %q is malformed (ignoring): %v", path, err)
		})
		return SmartReport{Present: false}, nil
	}

	disks := make([]SmartDisk, 0, len(raw.Disks))
	for _, d := range raw.Disks {
		disks = append(disks, SmartDisk{
			Device:          d.Device,
			Model:           d.Model,
			Serial:          d.Serial,
			Health:          d.Health,
			Reallocated:     d.Reallocated,
			Pending:         d.Pending,
			Uncorrectable:   d.Uncorrectable,
			CRCErrors:       d.CRCErrors,
			TempC:           d.TempC,
			PowerOnHours:    d.PowerOnHours,
			PowerCycleCount: d.PowerCycleCount,
		})
	}

	return SmartReport{
		Disks:    disks,
		SourceAt: smartSourceAt(path, raw.GeneratedAt),
		Present:  true,
	}, nil
}

// smartSourceAt resolves the report timestamp: the JSON generated_at when it
// parses, else the file's mtime, else now.
func smartSourceAt(path, generatedAt string) time.Time {
	if generatedAt != "" {
		if t, err := time.Parse(time.RFC3339, generatedAt); err == nil {
			return t.UTC()
		}
	}
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime().UTC()
	}
	return time.Now().UTC()
}

// RollupHealth classifies one disk's health as ok | warn | fail per the spec:
//   - fail — overall health FAILED, or pending > 0, or uncorrectable > 0.
//   - warn — reallocated > 0, or temperature above the soft ceiling.
//   - ok   — otherwise.
//
// It lives here (rather than in the server) so it is unit-testable, and is
// called by the DTO builder so the UI receives a per-disk rollup string.
func RollupHealth(health string, reallocated, pending, uncorrectable uint64, tempC int) string {
	if strings.EqualFold(health, "FAILED") || pending > 0 || uncorrectable > 0 {
		return "fail"
	}
	if reallocated > 0 || tempC > SmartTempWarnCeilingC {
		return "warn"
	}
	return "ok"
}
