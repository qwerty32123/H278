package Memory

type SharedMemoryInterface interface {
	WriteData(data []byte) error
	Close()
}

func NewSharedMemory(name string, size int) (SharedMemoryInterface, error) {
	return newPlatformSharedMemory(name, size)
}
