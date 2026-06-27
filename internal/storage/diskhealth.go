package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// RaidArrayRow is one software-RAID array's state for a node, as a
// replace-on-write snapshot (see ReplaceRaidArrays). RAID is read live each
// cycle and is never marked stale.
type RaidArrayRow struct {
	Node          string
	Name          string
	Level         string
	State         string
	TotalDevices  int
	ActiveDevices int
	ResyncPercent float64
	Detail        string
	At            time.Time
}

// SmartDiskRow is one disk's SMART health for a node, as a replace-on-write
// snapshot (see ReplaceSmartDisks). SourceAt is when the underlying SMART file
// was generated, used by the server to flag stale data.
type SmartDiskRow struct {
	Node            string
	Device          string
	Model           string
	Serial          string
	Health          string
	Reallocated     uint64
	Pending         uint64
	Uncorrectable   uint64
	CRCErrors       uint64
	TempC           int
	PowerOnHours    uint64
	PowerCycleCount uint64
	SourceAt        time.Time
	At              time.Time
}

// ReplaceRaidArrays atomically replaces a node's RAID arrays: it deletes the
// node's existing rows and inserts the given ones in one transaction. An empty
// slice simply clears the node — so a host that loses (or never had) RAID goes
// empty, which is what lets the UI hide the subsection independently.
func (db *DB) ReplaceRaidArrays(node string, rows []RaidArrayRow) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin replace raid arrays for node %q: %w", node, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM raid_array WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: clear raid arrays for node %q: %w", node, err)
	}

	const q = `
INSERT INTO raid_array (node, name, level, state, total_devices, active_devices, resync_percent, detail, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`
	for _, r := range rows {
		if _, err := tx.Exec(
			q,
			node, r.Name, r.Level, r.State,
			r.TotalDevices, r.ActiveDevices, r.ResyncPercent, r.Detail,
			r.At.UTC().Unix(),
		); err != nil {
			return fmt.Errorf("storage: insert raid array for node %q name %q: %w", node, r.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit raid arrays for node %q: %w", node, err)
	}
	return nil
}

// ReplaceSmartDisks atomically replaces a node's SMART disks, mirroring
// ReplaceRaidArrays. An empty slice clears the node (SMART absent on this host).
func (db *DB) ReplaceSmartDisks(node string, rows []SmartDiskRow) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin replace smart disks for node %q: %w", node, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM smart_disk WHERE node = ?;`, node); err != nil {
		return fmt.Errorf("storage: clear smart disks for node %q: %w", node, err)
	}

	const q = `
INSERT INTO smart_disk (node, device, model, serial, health, reallocated, pending, uncorrectable, crc_errors, temp_c, power_on_hours, power_cycle_count, source_at, at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`
	for _, r := range rows {
		if _, err := tx.Exec(
			q,
			node, r.Device, r.Model, r.Serial, r.Health,
			int64(r.Reallocated), int64(r.Pending), int64(r.Uncorrectable), int64(r.CRCErrors),
			r.TempC, int64(r.PowerOnHours), int64(r.PowerCycleCount),
			r.SourceAt.UTC().Unix(), r.At.UTC().Unix(),
		); err != nil {
			return fmt.Errorf("storage: insert smart disk for node %q device %q: %w", node, r.Device, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit smart disks for node %q: %w", node, err)
	}
	return nil
}

// RaidArrays returns a node's stored RAID arrays ordered by name. Empty if none.
func (db *DB) RaidArrays(node string) ([]RaidArrayRow, error) {
	const q = `
SELECT node, name, level, state, total_devices, active_devices, resync_percent, detail, at
FROM raid_array
WHERE node = ?
ORDER BY name ASC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query raid arrays for node %q: %w", node, err)
	}
	defer rows.Close()

	out, err := scanRaidArrays(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan raid arrays for node %q: %w", node, err)
	}
	return out, nil
}

// SmartDisks returns a node's stored SMART disks ordered by device. Empty if none.
func (db *DB) SmartDisks(node string) ([]SmartDiskRow, error) {
	const q = `
SELECT node, device, model, serial, health, reallocated, pending, uncorrectable, crc_errors, temp_c, power_on_hours, power_cycle_count, source_at, at
FROM smart_disk
WHERE node = ?
ORDER BY device ASC;`
	rows, err := db.conn.Query(q, node)
	if err != nil {
		return nil, fmt.Errorf("storage: query smart disks for node %q: %w", node, err)
	}
	defer rows.Close()

	out, err := scanSmartDisks(rows)
	if err != nil {
		return nil, fmt.Errorf("storage: scan smart disks for node %q: %w", node, err)
	}
	return out, nil
}

func scanRaidArrays(rows *sql.Rows) ([]RaidArrayRow, error) {
	var out []RaidArrayRow
	for rows.Next() {
		var (
			r      RaidArrayRow
			atUnix int64
		)
		if err := rows.Scan(
			&r.Node, &r.Name, &r.Level, &r.State,
			&r.TotalDevices, &r.ActiveDevices, &r.ResyncPercent, &r.Detail,
			&atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		r.At = time.Unix(atUnix, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}

func scanSmartDisks(rows *sql.Rows) ([]SmartDiskRow, error) {
	var out []SmartDiskRow
	for rows.Next() {
		var (
			r                                       SmartDiskRow
			realloc, pending, uncorr, crc, poh, pcc int64
			sourceUnix, atUnix                      int64
		)
		if err := rows.Scan(
			&r.Node, &r.Device, &r.Model, &r.Serial, &r.Health,
			&realloc, &pending, &uncorr, &crc, &r.TempC, &poh, &pcc,
			&sourceUnix, &atUnix,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		r.Reallocated = uint64(realloc)
		r.Pending = uint64(pending)
		r.Uncorrectable = uint64(uncorr)
		r.CRCErrors = uint64(crc)
		r.PowerOnHours = uint64(poh)
		r.PowerCycleCount = uint64(pcc)
		r.SourceAt = time.Unix(sourceUnix, 0).UTC()
		r.At = time.Unix(atUnix, 0).UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return out, nil
}
