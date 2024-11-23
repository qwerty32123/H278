//go:build !windows

package Unix

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

type UnixSharedMemory struct {
	fd   int
	data []byte
	size int
	name string
}

func NewUnixSharedMemory(name string, size int) (*UnixSharedMemory, error) {
	// Ensure name starts with /
	if name[0] != '/' {
		name = "/" + name
	}

	// Use OpenFile with O_TMPFILE for shared memory
	fd, err := unix.Open("/dev/shm", unix.O_RDWR|unix.O_CREAT, 0666)
	if err != nil {
		return nil, fmt.Errorf("open error: %v", err)
	}

	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		unix.Close(fd)
		os.Remove("/dev/shm" + name)
		return nil, fmt.Errorf("ftruncate error: %v", err)
	}

	data, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		os.Remove("/dev/shm" + name)
		return nil, fmt.Errorf("mmap error: %v", err)
	}

	return &UnixSharedMemory{
		fd:   fd,
		data: data,
		size: size,
		name: name,
	}, nil
}

func (u *UnixSharedMemory) WriteData(data []byte) error {
	if len(data)+4 > u.size {
		return fmt.Errorf("data size exceeds shared memory size")
	}

	// Write length prefix and data
	copy(u.data[0:4], uint32ToBytes(uint32(len(data))))
	copy(u.data[4:], data)

	// Sync changes
	if err := unix.Msync(u.data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("msync error: %v", err)
	}

	return nil
}

func (u *UnixSharedMemory) ReadData() ([]byte, error) {
	if len(u.data) < 4 {
		return nil, fmt.Errorf("shared memory too small to contain length prefix")
	}

	// Read length prefix
	length := bytesToUint32(u.data[0:4])

	if int(length)+4 > len(u.data) {
		return nil, fmt.Errorf("invalid data length in shared memory")
	}

	// Make a copy of the data
	result := make([]byte, length)
	copy(result, u.data[4:4+length])

	return result, nil
}

func (u *UnixSharedMemory) Close() {
	if u.data != nil {
		unix.Munmap(u.data)
		u.data = nil
	}
	if u.fd != 0 {
		unix.Close(u.fd)
		u.fd = 0
	}
	if u.name != "" {
		os.Remove("/dev/shm" + u.name)
		u.name = ""
	}
}

func uint32ToBytes(n uint32) []byte {
	b := make([]byte, 4)
	b[0] = byte(n)
	b[1] = byte(n >> 8)
	b[2] = byte(n >> 16)
	b[3] = byte(n >> 24)
	return b
}

func bytesToUint32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}
