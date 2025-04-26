package main

import (
	"fmt"

	"github.com/n1/n1/internal/log"
)

const version = "0.0.1-dev"

func main() {
	fmt.Println("bosr version", version)
	log.L.Info().Msg("bosr started")
}
