//go:build darwin

package main

import (
	"os"
	"syscall"
	"time"
)

// statBirthTime reads an inode's st_birthtime. macOS is the platform the warden
// actually runs on, and the only one where the anchor identity question is even
// meaningful (TCC), so this is where the real implementation lives.
func statBirthTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, errNoBirthTime
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec), nil
}
