//go:build linux

package i2c

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctl requests from the kernel's include/uapi/linux/i2c-dev.h.
const (
	iocRdwr = 0x0707 // I2C_RDWR: a combined transaction
)

// Message flags from include/uapi/linux/i2c.h.
const (
	msgRead = 0x0001 // I2C_M_RD
)

// i2cMsg mirrors struct i2c_msg. The explicit padding makes the pointer's
// alignment visible rather than implied; the layout matches C on both 32- and
// 64-bit ARM.
type i2cMsg struct {
	addr  uint16
	flags uint16
	len   uint16
	_     uint16
	buf   uintptr
}

// i2cRdwrData mirrors struct i2c_rdwr_ioctl_data.
type i2cRdwrData struct {
	msgs  uintptr
	nmsgs uint32
}

// Bus is an open I2C bus.
type Bus struct {
	mu sync.Mutex
	f  *os.File
}

// Open opens an I2C bus by device path, e.g. "/dev/i2c-1".
func Open(path string) (*Bus, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("i2c: open %s: %w", path, err)
	}
	return &Bus{f: f}, nil
}

// ReadReg16 reads n bytes from a device, after writing a 16-bit big-endian
// register address.
//
// Both halves go in ONE ioctl, which is what makes the kernel issue a repeated
// start rather than a stop between them. Splitting it into a write followed by
// a read is the mistake that makes this EEPROM return garbage.
func (b *Bus) ReadReg16(addr uint16, reg uint16, n int) ([]byte, error) {
	if n <= 0 {
		return nil, fmt.Errorf("i2c: read length must be positive, got %d", n)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.f == nil {
		return nil, fmt.Errorf("i2c: read: bus is closed")
	}

	regBytes := [2]byte{byte(reg >> 8), byte(reg)}
	out := make([]byte, n)

	msgs := [2]i2cMsg{
		{addr: addr, flags: 0, len: 2, buf: uintptr(unsafe.Pointer(&regBytes[0]))},
		{addr: addr, flags: msgRead, len: uint16(n), buf: uintptr(unsafe.Pointer(&out[0]))},
	}
	data := i2cRdwrData{msgs: uintptr(unsafe.Pointer(&msgs[0])), nmsgs: 2}

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, b.f.Fd(), iocRdwr, uintptr(unsafe.Pointer(&data)))

	// Keep the buffers alive until the kernel has finished with them: the
	// uintptr fields above are invisible to the garbage collector.
	runtimeKeepAlive(&regBytes, &out, &msgs)

	if errno != 0 {
		return nil, fmt.Errorf("i2c: combined read from 0x%02X reg 0x%04X: %w", addr, reg, errno)
	}
	return out, nil
}

// Close releases the bus. It is safe to call more than once.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.f == nil {
		return nil
	}
	err := b.f.Close()
	b.f = nil
	if err != nil {
		return fmt.Errorf("i2c: close: %w", err)
	}
	return nil
}

// The ioctl structs go straight to the kernel, so their layout must match the
// C definitions exactly. A mismatch does not fail loudly — the kernel reads a
// pointer from the wrong offset and the transfer returns garbage, which looks
// exactly like the seating fault the bench already chased once on good
// hardware.
//
// These are compile-time assertions, so `GOARCH=arm go build` proves the
// 32-bit layout without anyone owning a Pi Zero to run it on. If a size is
// wrong, one of the subtractions below is negative and converting it to uint
// will not compile.
const (
	ptrSize = unsafe.Sizeof(uintptr(0))

	// struct i2c_msg: u16 addr, u16 flags, u16 len, then a pointer aligned
	// to its own width. 12 bytes on 32-bit, 16 on 64-bit.
	wantMsgSize = ((6+ptrSize-1)/ptrSize)*ptrSize + ptrSize

	// struct i2c_rdwr_ioctl_data: a pointer then a u32, padded to the
	// pointer's alignment. 8 bytes on 32-bit, 16 on 64-bit.
	wantRdwrSize = ((ptrSize + 4 + ptrSize - 1) / ptrSize) * ptrSize
)

const (
	_ = uint(unsafe.Sizeof(i2cMsg{}) - wantMsgSize)
	_ = uint(wantMsgSize - unsafe.Sizeof(i2cMsg{}))
	_ = uint(unsafe.Sizeof(i2cRdwrData{}) - wantRdwrSize)
	_ = uint(wantRdwrSize - unsafe.Sizeof(i2cRdwrData{}))

	// The pointer must sit at offset 8 in i2c_msg on every platform we
	// build for; anything else means the kernel reads a shifted address.
	_ = uint(unsafe.Offsetof(i2cMsg{}.buf) - 8)
	_ = uint(8 - unsafe.Offsetof(i2cMsg{}.buf))
)
