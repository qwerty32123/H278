//go:build windows

package Memory

import "H278/Memory/Windows"

func newPlatformSharedMemory(name string, size int) (SharedMemoryInterface, error) {
	return Windows.NewWindowsSharedMemory(name, size)
}
