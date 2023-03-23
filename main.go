package main

import (
	"log"

	"github.com/deitch/go-sources-and-licenses/cmd"
)

func main() {
	if err := cmd.New().Execute(); err != nil {
		log.Fatalf("error during command execution: %v", err)
	}
}
