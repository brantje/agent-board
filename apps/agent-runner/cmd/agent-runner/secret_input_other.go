//go:build !linux

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
)

func readRegistrationToken(reader *bufio.Reader, input io.Reader, _ io.Writer) (string, error) {
	if file, ok := input.(*os.File); ok {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return "", errors.New("secure interactive registration token input is only supported on Linux")
		}
	}
	return reader.ReadString('\n')
}
