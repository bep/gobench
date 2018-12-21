package testing

import (
	"testing"
)

func BenchmarkSleep(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sleep()
	}
}
