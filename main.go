package main

import (
	"log"

	"github.com/p2pquake/jmaxml-seis-parser-relay/cmd"
)

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
}

func main() {
	cmd.Execute()
}
