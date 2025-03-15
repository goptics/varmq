# GoCQ: High-Performance Concurrent Queue for Gophers

Package gocq offers a concurrent queue system using channels and goroutines, supporting both FIFO and priority operations, with options for result-returning and void (non-returning) queues.
Zero dependency just install, import and use any where in your go program.

[![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8.svg?logo=go)](https://pkg.go.dev/github.com/fahimfaisaal/gocq)
[![Go Report Card](https://goreportcard.com/badge/github.com/fahimfaisaal/gocq)](https://goreportcard.com/report/github.com/fahimfaisaal/gocq)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://golang.org/doc/devel/release.html)
[![CI](https://github.com/fahimfaisaal/gocq/actions/workflows/go.yml/badge.svg)](https://github.com/fahimfaisaal/gocq/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/fahimfaisaal/gocq/branch/main/graph/badge.svg)](https://codecov.io/gh/fahimfaisaal/gocq)
![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/fahimfaisaal/gocq?utm_source=oss&utm_medium=github&utm_campaign=fahimfaisaal%2Fgocq&labelColor=171717&color=FF570A&link=https%3A%2F%2Fcoderabbit.ai&label=CodeRabbit+Reviews)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)

## 🌟 Features

- Generic type support for both data and results
- Result returning and void (non-returning) queue variants
- Configurable concurrency limits with auto-scaling based on CPU cores
- FIFO queue with O(1) operations
- Priority queue support with O(log n) operations
- Non-blocking job submission
- Control job after submission
- Thread-safe operations
- Pause/Resume functionality
- Clean and graceful shutdown mechanisms

## 🔧 Installation

```bash
go get github.com/fahimfaisaal/gocq
```

## 🚀 Quick Start

```go
package main

import (
    "fmt"
    "time"
    "github.com/fahimfaisaal/gocq"
)

func main() {
    // Create a queue with 2 concurrent workers
    queue := gocq.NewQueue(2, func(data int) (int, error) {
        time.Sleep(500 * time.Millisecond)
        return data * 2, nil
    })
    defer queue.Close()

    // Add a single job
    result, err := queue.Add(5).WaitForResult()
    fmt.Println(result, err) // Output: 10 nil

    // Add multiple jobs
    for result := range queue.AddAll([]int{1, 2, 3, 4, 5}).Results() {
        if result.Err != nil {
            fmt.Printf("Error: %v\n", result.Err)
            continue
        }
        fmt.Println(result.Data) // Output: 2, 4, 6, 8, 10 (unordered)
    }
}
```

## 💡 Examples

### Priority Queue

```go
queue := gocq.NewPriorityQueue(1, func(data int) (int, error) {
    time.Sleep(500 * time.Millisecond)
    return data * 2, nil
})
defer queue.WaitAndClose()

items := []gocq.PQItem[int]{
    {Value: 1, Priority: 2}, // Lowest priority
    {Value: 2, Priority: 1}, // Medium priority
    {Value: 3}, // Highest priority
}

for result := range queue.AddAll(items).Results() {
    fmt.Println(result.Data) // Output: 6, 4, 2 (processed by priority)
}
```

### Void Queue

```go
queue := gocq.NewVoidQueue(2, func(data int) error {
    fmt.Printf("Processing: %d\n", data)
    time.Sleep(500 * time.Millisecond)
    fmt.Printf("Processed: %d\n", data)
    return nil
})
defer queue.WaitAndClose()

queue.Add(1).Drain()
queue.AddAll([]int{2, 3, 4}).Drain()
```

## 🚀 Performance

### Benchmark Results

```bash
# Normal Queue

goos: linux
goarch: amd64
pkg: github.com/fahimfaisaal/gocq/internal/concurrent_queue
cpu: 13th Gen Intel(R) Core(TM) i7-13700
BenchmarkQueue_Operations/Add-24                         1113538              1467 ns/op             376 B/op          8 allocs/op
BenchmarkQueue_Operations/AddAll-24                        15778            131428 ns/op           17353 B/op        510 allocs/op
BenchmarkPriorityQueue_Operations/Add-24                 1382113             878.6 ns/op             352 B/op          8 allocs/op
BenchmarkPriorityQueue_Operations/AddAll-24                10000            121044 ns/op           14951 B/op        510 allocs/op
```

```bash
# Void Queue

goos: linux
goarch: amd64
pkg: github.com/fahimfaisaal/gocq/internal/concurrent_queue/void_queue
cpu: 13th Gen Intel(R) Core(TM) i7-13700
BenchmarkVoidQueue_Operations/Add-24                     1987044              1001 ns/op             240 B/op          7 allocs/op
BenchmarkVoidQueue_Operations/AddAll-24                    10000            134711 ns/op           16989 B/op        512 allocs/op
BenchmarkVoidPriorityQueue_Operations/Add-24             1326402              1323 ns/op             240 B/op          7 allocs/op
BenchmarkVoidPriorityQueue_Operations/AddAll-24            11812            108261 ns/op           16965 B/op        512 allocs/op
```

| Queue Type         | Operation | Variant | ns/op | B/op | allocs/op |
| ------------------ | --------- | ------- | ----- | ---- | --------- |
| Non-Priority Queue | Add       | Normal  | 1467  | 376  | 8         |
| Non-Priority Queue | Add       | Void    | 1001  | 240  | 7         |

> Note: Void queue is ~25% faster than the standard queue according to benchmarks. even the memory usage is also lower.

### Run Benchmarks

```bash
go test -bench=. -benchmem ./internal/concurrent_queue/
go test -bench=. -benchmem ./internal/concurrent_queue/void_queue/
```

## 👤 Author

- GitHub: [@fahimfaisaal](https://github.com/fahimfaisaal)
- LinkedIn: [in/fahimfaisaal](https://www.linkedin.com/in/fahimfaisaal/)
- Twitter: [@FahimFaisaal](https://twitter.com/FahimFaisaal)

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
