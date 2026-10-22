package cif

import "runtime"

type runtimeMemStats = runtime.MemStats

func readMemStats(stats *runtime.MemStats) {
	runtime.GC()
	runtime.ReadMemStats(stats)
}
