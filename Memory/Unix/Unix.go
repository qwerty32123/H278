//go:build !windows

package Unix

import (
	"fmt"
	"golang.org/x/sys/unix"
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

	fd, err := unix.Shm_open(name, unix.O_RDWR|unix.O_CREAT, 0666)
	if err != nil {
		return nil, fmt.Errorf("shm_open error: %v", err)
	}

	if err := unix.Ftruncate(fd, int64(size)); err != nil {
		unix.Close(fd)
		unix.Shm_unlink(name)
		return nil, fmt.Errorf("ftruncate error: %v", err)
	}

	data, err := unix.Mmap(fd, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		unix.Close(fd)
		unix.Shm_unlink(name)
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
		unix.Shm_unlink(u.name)
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
