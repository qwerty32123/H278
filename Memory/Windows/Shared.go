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

func NewSharedMemoryClient(name string, size int) (*SharedMemoryClient, error) {
	handle, err := OpenFileMapping(FILE_MAP_ALL_ACCESS, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return nil, fmt.Errorf("OpenFileMapping error: %v", err)
	}

	view, err := windows.MapViewOfFile(handle, FILE_MAP_ALL_ACCESS, 0, 0, uintptr(size))
	if err != nil {
		err := windows.CloseHandle(handle)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("MapViewOfFile error: %v", err)
	}

	eventName := name + "_event"
	eventHandle, err := OpenEvent(windows.EVENT_MODIFY_STATE, false, windows.StringToUTF16Ptr(eventName))
	if err != nil {
		windows.UnmapViewOfFile(view)
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("OpenEvent error: %v", err)
	}

	return &SharedMemoryClient{
		handle:      handle,
		view:        view,
		size:        size,
		eventHandle: eventHandle,
	}, nil
}

func (c *SharedMemoryClient) WriteData(data []byte) error {
	totalSize := 4 + len(data) // 4 bytes for length prefix
	if totalSize > c.size {
		return fmt.Errorf("data size exceeds shared memory size")
	}

	dest := unsafe.Slice((*byte)(unsafe.Pointer(c.view)), totalSize)
	binary.LittleEndian.PutUint32(dest[:4], uint32(len(data)))
	copy(dest[4:], data)

	err := windows.SetEvent(c.eventHandle)
	if err != nil {
		return err
	}
	return nil
}

func (c *SharedMemoryClient) Close() {
	if c.view != 0 {
		err := windows.UnmapViewOfFile(c.view)
		if err != nil {
			return
		}
	}
	if c.handle != 0 {
		err := windows.CloseHandle(c.handle)
		if err != nil {
			return
		}
	}
	if c.eventHandle != 0 {
		err := windows.CloseHandle(c.eventHandle)
		if err != nil {
			return
		}
	}
}
