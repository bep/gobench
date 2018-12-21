package testing

import (
	"time"
)

var d time.Duration = 20 * time.Millisecond

func sleep() {
	time.Sleep(d)
}
