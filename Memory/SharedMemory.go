package Memory

type SharedMemoryInterface interface {
	WriteData(clientID uint32, data []byte) error
	Close() error
}

func NewSharedMemory(name string, size int) (SharedMemoryInterface, error) {
	return newPlatformSharedMemory(name, size)
}
