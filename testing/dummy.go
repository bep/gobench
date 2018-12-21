package testing

import (
	"time"
)

var d time.Duration = 10 * time.Millisecond

func sleep() {
	time.Sleep(d)
}
