package testing

import (
	"time"
)

var (
	d    time.Duration = 10 * time.Millisecond
	size               = 10
)

func sleep() {
	a := make([]int, size)
	if len(a) == 0 {

	}
	time.Sleep(d)
}
