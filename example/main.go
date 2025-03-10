package main

import (
	"fmt"
	"time"
)

func main() {
	ch1 := make(chan int)
	ch2 := make(chan string)

	// Simulate sending data on ch1 and ch2 in separate goroutines.
	go func() {
		for i := 1; i <= 5; i++ {
			ch1 <- i
			time.Sleep(500 * time.Millisecond)
		}
		close(ch1) // Close ch1 after sending all data
	}()

	go func() {
		words := []string{"alpha", "beta", "gamma", "delta"}
		for _, word := range words {
			ch2 <- word
			time.Sleep(700 * time.Millisecond)
		}
		close(ch2) // Close ch2 after sending all data
	}()

	// Using select in a loop to range over both channels.
	for {
		// If both channels are nil (closed), exit the loop.
		if ch1 == nil && ch2 == nil {
			break
		}

		select {
		case num, ok := <-ch1:
			if !ok {
				// ch1 is closed, set it to nil so select won't choose it again.
				ch1 = nil
				continue
			}
			fmt.Println("Received from ch1:", num)
		case word, ok := <-ch2:
			if !ok {
				// ch2 is closed, set it to nil.
				ch2 = nil
				continue
			}
			fmt.Println("Received from ch2:", word)
		}
	}

	fmt.Println("Both channels are closed. Exiting.")
}
