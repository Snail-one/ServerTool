package ui

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

func hideInputEcho(input *os.File) (func(), error) {
	if input == nil {
		return nil, nil
	}
	fd := input.Fd()
	var original syscall.Termios
	if err := terminalAttributes(fd, syscall.TCGETS, &original); err != nil {
		if errors.Is(err, syscall.ENOTTY) {
			return nil, nil
		}
		return nil, err
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	hidden := original
	hidden.Lflag &^= syscall.ECHO | syscall.ECHONL
	if err := terminalAttributes(fd, syscall.TCSETS, &hidden); err != nil {
		signal.Stop(interrupts)
		return nil, err
	}

	done := make(chan struct{})
	var once sync.Once
	restore := func() {
		once.Do(func() {
			_ = terminalAttributes(fd, syscall.TCSETS, &original)
			signal.Stop(interrupts)
			close(done)
		})
	}
	go func() {
		select {
		case sig := <-interrupts:
			// Restore the terminal before passing the signal back to the process.
			restore()
			_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
		case <-done:
		}
	}()
	return restore, nil
}

func terminalAttributes(fd, request uintptr, attributes *syscall.Termios) error {
	for {
		_, _, err := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(attributes)))
		switch err {
		case 0:
			return nil
		case syscall.EINTR:
			continue
		default:
			return err
		}
	}
}
