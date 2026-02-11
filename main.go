package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

func run() error {
	target := flag.String("target", "", "registry reference (e.g. localhost:5000/deck:v1)")
	local := flag.String("local", "", "output OCI layout directory (instead of pushing to registry)")
	deck := flag.String("deck", "", "path to deck definition file")
	images := flag.String("images", "PNG-cards-1.3", "path to card PNG directory")
	plainHTTP := flag.Bool("plain-http", false, "use HTTP instead of HTTPS")
	flag.Parse()

	ctx := context.Background()

	switch {
	case *local != "":
		tag := "latest"
		if *target != "" {
			tag = parseTag(*target)
		}
		return saveDeckLocal(ctx, *local, *deck, *images, tag)
	case *target != "":
		return pushDeck(ctx, *target, *deck, *images, *plainHTTP)
	default:
		return fmt.Errorf("either --target or --local is required")
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
