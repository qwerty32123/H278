//go:build windows

package Windows

import (
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

const (
	FILE_MAP_ALL_ACCESS = 0xF001F
)

type SharedMemoryClient struct {
	handle      windows.Handle
	view        uintptr
	size        int
	eventHandle windows.Handle
}

type WindowsSharedMemory struct {
	handle      windows.Handle
	view        uintptr
	size        int
	eventHandle windows.Handle
}

func OpenEvent(desiredAccess uint32, inheritHandle bool, name *uint16) (windows.Handle, error) {
	modkernel32 := windows.NewLazyDLL("kernel32.dll")
	procOpenEvent := modkernel32.NewProc("OpenEventW")

	handle, _, err := procOpenEvent.Call(
		uintptr(desiredAccess),
		uintptr(boolToInt(inheritHandle)),
		uintptr(unsafe.Pointer(name)),
	)

	if handle == 0 {
		return 0, err
	}
	return windows.Handle(handle), nil
}

func OpenFileMapping(desiredAccess uint32, inheritHandle bool, name *uint16) (windows.Handle, error) {
	modkernel32 := windows.NewLazyDLL("kernel32.dll")
	procOpenFileMapping := modkernel32.NewProc("OpenFileMappingW")

	handle, _, err := procOpenFileMapping.Call(
		uintptr(desiredAccess),
		uintptr(boolToInt(inheritHandle)),
		uintptr(unsafe.Pointer(name)),
	)

	if handle == 0 {
		return 0, err
	}
	return windows.Handle(handle), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
func NewWindowsSharedMemory(name string, size int) (*WindowsSharedMemory, error) {
	handle, err := OpenFileMapping(FILE_MAP_ALL_ACCESS, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return nil, fmt.Errorf("OpenFileMapping error: %v", err)
	}

	view, err := windows.MapViewOfFile(handle, FILE_MAP_ALL_ACCESS, 0, 0, uintptr(size))
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("MapViewOfFile error: %v", err)
	}

	eventName := name + "_event"
	eventHandle, err := OpenEvent(windows.EVENT_MODIFY_STATE, false, windows.StringToUTF16Ptr(eventName))
	if err != nil {
		windows.UnmapViewOfFile(view)
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("OpenEvent error: %v", err)
	}

	return &WindowsSharedMemory{
		handle:      handle,
		view:        view,
		size:        size,
		eventHandle: eventHandle,
	}, nil
}

func (w *WindowsSharedMemory) WriteData(data []byte) error {
	totalSize := 4 + len(data)
	if totalSize > w.size {
		return fmt.Errorf("data size exceeds shared memory size")
	}

	dest := unsafe.Slice((*byte)(unsafe.Pointer(w.view)), totalSize)
	binary.LittleEndian.PutUint32(dest[:4], uint32(len(data)))
	copy(dest[4:], data)

	return windows.SetEvent(w.eventHandle)
}

func (w *WindowsSharedMemory) Close() {
	if w.view != 0 {
		windows.UnmapViewOfFile(w.view)
	}
	if w.handle != 0 {
		windows.CloseHandle(w.handle)
	}
	if w.eventHandle != 0 {
		windows.CloseHandle(w.eventHandle)
	}
}
