package httpapi_test

import "runtime"

func runtimeGC() { runtime.GC() }

func heapInuse() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}
