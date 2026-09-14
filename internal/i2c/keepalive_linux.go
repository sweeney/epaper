//go:build linux

package i2c

import "runtime"

// runtimeKeepAlive stops the garbage collector reclaiming buffers whose only
// remaining reference is a uintptr inside a struct the kernel is reading.
func runtimeKeepAlive(vs ...any) {
	for _, v := range vs {
		runtime.KeepAlive(v)
	}
}
