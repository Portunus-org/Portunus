// Package sysinfo reports the panel's own resource usage.
package sysinfo

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type Memory struct {
	// RSSBytes is the resident set size from /proc (0 where unavailable).
	RSSBytes uint64 `json:"rssBytes"`
	// HeapBytes is live Go heap memory.
	HeapBytes uint64 `json:"heapBytes"`
	// SysBytes is total memory obtained from the OS by the Go runtime.
	SysBytes   uint64 `json:"sysBytes"`
	Goroutines int    `json:"goroutines"`
}

func ReadMemory() Memory {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return Memory{
		RSSBytes:   readRSS(),
		HeapBytes:  ms.HeapAlloc,
		SysBytes:   ms.Sys,
		Goroutines: runtime.NumGoroutine(),
	}
}

func readRSS() uint64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "VmRSS:")
		if !ok {
			continue
		}
		kb, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
