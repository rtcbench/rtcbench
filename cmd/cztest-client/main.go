package main

import (
	"log"
	"os"
	"strconv"

	"call.zip/internal/cztest_client"
)

func main() {
	log.Println("=== cztest-client (prototype) ===")

	if len(os.Args) < 3 {
		usage()
	}

	ip := os.Args[1]
	port, err := strconv.Atoi(os.Args[2])

	if err != nil {
		log.Println("Invalid port")
		usage()
	}

	cztest_client.StartTestClient(&cztest_client.ClientArgs{
		ServerIP:   ip,
		ServerPort: port,
	})
}

func usage() {
	log.Fatalf("Usage: %s <ip> <port>", os.Args[0])
}
