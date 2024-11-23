//go:build !windows

package Memory

import "H278/Memory/Unix"

func newPlatformSharedMemory(name string, size int) (SharedMemoryInterface, error) {
	return Unix.NewUnixSharedMemory(name, size)
}
