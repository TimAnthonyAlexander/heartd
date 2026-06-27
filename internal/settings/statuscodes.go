package settings

import (
	"sort"
	"strconv"
	"strings"
)

// parseStatusCodes splits a comma-joined status-code string (the storage
// representation, e.g. "200,401,403") into a slice of ints. Blank or
// unparseable entries are skipped so a malformed stored value can never crash a
// check.
func parseStatusCodes(s string) []int {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// formatStatusCodes joins a slice of status codes into the comma-separated
// storage representation. It returns "" for an empty slice.
func formatStatusCodes(codes []int) string {
	if len(codes) == 0 {
		return ""
	}
	parts := make([]string, len(codes))
	for i, c := range codes {
		parts[i] = strconv.Itoa(c)
	}
	return strings.Join(parts, ",")
}

// dedupeSortStatusCodes returns the codes with duplicates removed and ascending
// order, so a check's accepted-status list is canonical regardless of input
// order.
func dedupeSortStatusCodes(codes []int) []int {
	if len(codes) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(codes))
	var out []int
	for _, c := range codes {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}
