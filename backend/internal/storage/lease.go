package storage

import (
	"os"
	"syscall"
)

// Lease enforces one serving process per persistent data directory.
func (s *Store) Lease() (*os.File, error) {
	f, e := s.root.OpenFile("runtime.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, ErrStorage
	}
	return f, nil
}
