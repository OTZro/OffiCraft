//go:build !darwin

package main

import "time"

// statBirthTime has no portable answer off darwin: Linux's stat carries no
// creation time (statx does, but not through syscall.Stat_t), and mtime is NOT a
// substitute — it moves on any write and is settable, so using it would turn a
// deterministic negative into a guess. Refusing is the correct answer: the
// verdict simply loses its negative direction and can still reach effective /
// unproven. CI runs on linux, which is why this file exists at all.
func statBirthTime(string) (time.Time, error) {
	return time.Time{}, errNoBirthTime
}
