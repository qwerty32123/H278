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

	handle, _, errCode := procOpenEvent.Call(
		uintptr(desiredAccess),
		uintptr(boolToInt(inheritHandle)),
		uintptr(unsafe.Pointer(name)),
	)

	if handle == 0 {
		return 0, fmt.Errorf("OpenEvent failed with error code: %v", errCode)
	}
	return windows.Handle(handle), nil
}

func OpenFileMapping(desiredAccess uint32, inheritHandle bool, name *uint16) (windows.Handle, error) {
	modkernel32 := windows.NewLazyDLL("kernel32.dll")
	procOpenFileMapping := modkernel32.NewProc("OpenFileMappingW")

	handle, _, errCode := procOpenFileMapping.Call(
		uintptr(desiredAccess),
		uintptr(boolToInt(inheritHandle)),
		uintptr(unsafe.Pointer(name)),
	)

	if handle == 0 {
		return 0, fmt.Errorf("OpenFileMapping failed with error code: %v", errCode)
	}
	return windows.Handle(handle), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func NewWindowsSharedMemory(name string, size int) (*SharedMemoryClient, error) {
	// Open the file mapping
	handle, err := OpenFileMapping(FILE_MAP_ALL_ACCESS, false, windows.StringToUTF16Ptr(name))
	if err != nil {
		return nil, fmt.Errorf("OpenFileMapping error: %w", err)
	}

	// If mapping fails, ensure we clean up the handle
	view, err := windows.MapViewOfFile(handle, FILE_MAP_ALL_ACCESS, 0, 0, uintptr(size))
	if err != nil {
		closeErr := windows.CloseHandle(handle)
		if closeErr != nil {
			return nil, fmt.Errorf("MapViewOfFile error: %v, additional CloseHandle error: %v", err, closeErr)
		}
		return nil, fmt.Errorf("MapViewOfFile error: %v", err)
	}

	// Open the event with proper error handling
	eventName := name + "_event"
	eventHandle, err := OpenEvent(windows.EVENT_MODIFY_STATE, false, windows.StringToUTF16Ptr(eventName))
	if err != nil {
		// Clean up resources in case of error
		unmapErr := windows.UnmapViewOfFile(view)
		closeErr := windows.CloseHandle(handle)
		if unmapErr != nil || closeErr != nil {
			return nil, fmt.Errorf("OpenEvent error: %v, additional cleanup errors: unmap=%v, close=%v",
				err, unmapErr, closeErr)
		}
		return nil, fmt.Errorf("OpenEvent error: %v", err)
	}

	return &SharedMemoryClient{
		handle:      handle,
		view:        view,
		size:        size,
		eventHandle: eventHandle,
	}, nil
}

func (c *SharedMemoryClient) WriteData(clientID uint32, data []byte) error {
	// Total size includes: 4 bytes for total size + 4 bytes for client ID + data length
	dataSize := 4 + len(data) // client ID size + actual data
	totalSize := 4 + dataSize // total size prefix + data size

	if totalSize > c.size {
		return fmt.Errorf("data size (%d) exceeds shared memory size (%d)", totalSize, c.size)
	}

	// Create a safe slice with bounds checking
	header := struct {
		Data uintptr
		Len  int
		Cap  int
	}{c.view, totalSize, totalSize}
	dest := *(*[]byte)(unsafe.Pointer(&header))

	// Write total size (excluding the size field itself)
	binary.LittleEndian.PutUint32(dest[0:4], uint32(dataSize))

	// Write client ID
	binary.LittleEndian.PutUint32(dest[4:8], clientID)

	// Write actual data
	copy(dest[8:], data)

	if err := windows.SetEvent(c.eventHandle); err != nil {
		return fmt.Errorf("SetEvent failed: %w", err)
	}
	return nil
}

func (c *SharedMemoryClient) Close() error {
	var errors []error

	if c.view != 0 {
		if err := windows.UnmapViewOfFile(c.view); err != nil {
			errors = append(errors, fmt.Errorf("UnmapViewOfFile failed: %w", err))
		}
		c.view = 0
	}

	if c.handle != 0 {
		if err := windows.CloseHandle(c.handle); err != nil {
			errors = append(errors, fmt.Errorf("CloseHandle for mapping failed: %w", err))
		}
		c.handle = 0
	}

	if c.eventHandle != 0 {
		if err := windows.CloseHandle(c.eventHandle); err != nil {
			errors = append(errors, fmt.Errorf("CloseHandle for event failed: %w", err))
		}
		c.eventHandle = 0
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple errors during Close: %v", errors)
	}
	return nil
}
