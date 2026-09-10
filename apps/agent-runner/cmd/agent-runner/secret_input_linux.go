//go:build linux

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

func readRegistrationToken(reader *bufio.Reader, input io.Reader, output io.Writer) (string, error) {
	file, ok := input.(*os.File)
	if !ok {
		return reader.ReadString('\n')
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return reader.ReadString('\n')
	}

	fd := file.Fd()
	var original syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &original); err != nil {
		return "", fmt.Errorf("disable terminal echo: %w", err)
	}
	hidden := original
	hidden.Lflag &^= syscall.ECHO
	if err := ioctlTermios(fd, syscall.TCSETS, &hidden); err != nil {
		return "", fmt.Errorf("disable terminal echo: %w", err)
	}
	value, readErr := reader.ReadString('\n')
	restoreErr := ioctlTermios(fd, syscall.TCSETS, &original)
	fmt.Fprintln(output)
	if restoreErr != nil {
		return "", fmt.Errorf("restore terminal echo: %w", restoreErr)
	}
	return value, readErr
}

func ioctlTermios(fd uintptr, request uintptr, value *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(value)))
	if errno != 0 {
		return errno
	}
	return nil
}
