package main

import (
	"fmt"
	"time"
)

// ldflags будут вшиты Python скриптом
var (
	CompanyID  string
	ServerURL  string
	BuildSlug  string
)

func main() {
	fmt.Println("Agent starting...")
	fmt.Println("CompanyID:", CompanyID)
	fmt.Println("ServerURL:", ServerURL)
	fmt.Println("BuildSlug:", BuildSlug)

	// простой loop для демонстрации работы
	for {
		fmt.Println("Agent heartbeat:", time.Now())
		time.Sleep(10 * time.Second)
	}
}
