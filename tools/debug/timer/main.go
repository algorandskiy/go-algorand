package main

import (
	"fmt"
	"time"

	"github.com/loov/hrtime"
)

func main() {
	b := hrtime.NewBenchmark(100)
	for b.Next() {
		time.Sleep(50 * time.Microsecond)
	}
	fmt.Println(b.Histogram(10))
}
