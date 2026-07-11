// Package diskspace reports free disk space percentage for the low-disk
// scheduled-job check (#360). It shells out to `df -Pk` (POSIX output format)
// rather than syscall.Statfs so the same code path works identically on Linux
// and macOS dev boxes without per-OS struct-field differences.
package diskspace

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// FreePercent returns the percentage of free space (0-100) on the filesystem
// containing path, by shelling out to `df -Pk <path>`.
func FreePercent(path string) (float64, error) {
	out, err := exec.Command("df", "-Pk", path).Output()
	if err != nil {
		return 0, fmt.Errorf("diskspace: run df -Pk %s: %w", path, err)
	}
	return parseDfOutput(string(out))
}

// parseDfOutput parses POSIX `df -Pk` output:
//
//	Filesystem     1024-blocks      Used  Available Capacity Mounted on
//	/dev/disk3s1s1   974716400  10475840  600038360      2%   /
//
// Capacity is "Used / (Used + Available)" rounded and expressed like "2%";
// free = 100 - that integer. Pure function (no I/O) so it's deterministically
// unit-testable without shelling out in tests.
func parseDfOutput(output string) (float64, error) {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("diskspace: unexpected df output (fewer than 2 lines): %q", output)
	}

	// The data line is the last non-empty line (df -Pk output is exactly one
	// header line + one data line for a single-path query; tolerate a
	// trailing blank line defensively).
	dataLine := strings.TrimSpace(lines[len(lines)-1])
	if dataLine == "" && len(lines) >= 2 {
		dataLine = strings.TrimSpace(lines[len(lines)-2])
	}
	fields := strings.Fields(dataLine)
	if len(fields) < 5 {
		return 0, fmt.Errorf("diskspace: unexpected df data line (want >= 5 fields, got %d): %q", len(fields), dataLine)
	}

	capacityField := fields[4]
	capacityStr := strings.TrimSuffix(capacityField, "%")
	used, err := strconv.Atoi(capacityStr)
	if err != nil {
		return 0, fmt.Errorf("diskspace: capacity field %q is not a percentage: %w", capacityField, err)
	}

	return 100 - float64(used), nil
}
