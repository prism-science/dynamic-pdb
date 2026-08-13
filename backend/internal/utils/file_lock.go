package utils

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

type FileLock struct {
	path string
	file *os.File
}

func NewFileLock(path string) *FileLock {
	return &FileLock{path: path}
}

func (l *FileLock) Locked(fn func() error) error {
	_, err := l.locked(false, fn)
	if err != nil {
		return err
	}
	return nil
}

func (l *FileLock) TryLocked(fn func() error) (bool, error) {
	return l.locked(true, fn)
}

func (l *FileLock) locked(try bool, fn func() error) (locked bool, err error) {
	if l.path == "" {
		return false, errors.New("file lock path is empty")
	}
	if fn == nil {
		return false, errors.New("file lock callback is nil")
	}

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false, fmt.Errorf("open file lock: %w", err)
	}
	l.file = file
	defer func() {
		releaseErr := l.release()
		if releaseErr == nil {
			return
		}
		if err != nil {
			err = fmt.Errorf("run locked function: %w", errors.Join(err, releaseErr))
			return
		}
		err = fmt.Errorf("release file lock: %w", releaseErr)
	}()

	lockMode := syscall.LOCK_EX
	if try {
		lockMode |= syscall.LOCK_NB
	}
	fileDescriptor, err := flockFileDescriptor(file)
	if err != nil {
		return false, err
	}
	if err := syscall.Flock(fileDescriptor, lockMode); err != nil {
		if try && (errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)) {
			return false, nil
		}
		return false, fmt.Errorf("lock file: %w", err)
	}

	if err := fn(); err != nil {
		return true, fmt.Errorf("run locked function: %w", err)
	}
	return true, nil
}

func (l *FileLock) release() error {
	if l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil

	fileDescriptor, err := flockFileDescriptor(file)
	if err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			return fmt.Errorf("read file descriptor: %w", errors.Join(err, closeErr))
		}
		return fmt.Errorf("read file descriptor: %w", err)
	}
	if err := syscall.Flock(fileDescriptor, syscall.LOCK_UN); err != nil {
		closeErr := file.Close()
		if closeErr != nil {
			return fmt.Errorf("unlock file: %w", errors.Join(err, closeErr))
		}
		return fmt.Errorf("unlock file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close file lock: %w", err)
	}
	return nil
}

func flockFileDescriptor(file *os.File) (int, error) {
	fileDescriptor := file.Fd()
	if strconv.IntSize == 32 && fileDescriptor > 1<<31-1 {
		return 0, fmt.Errorf("file descriptor overflows int: %d", fileDescriptor)
	}
	//nolint:gosec // The overflow case is checked above for 32-bit platforms.
	return int(fileDescriptor), nil
}
