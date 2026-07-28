package main

import (
	"flag"
	"fmt"
	"log"
)

func main() {
	source := flag.String("source", "", "Directory to build into an OCI image layer")
	output := flag.String("output", "oci-image", "Output directory for OCI image layout")
	tarOut := flag.String("tar", "", "Create a flat tarball from --source and write to this path")
	verbose := flag.Bool("verbose", false, "Enable verbose output")
	flag.Parse()

	if *source == "" {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: go run . --source <dir> [--output <dir>]")
		fmt.Fprintln(flag.CommandLine.Output(), "       go run . --source <dir> --tar <output.tar>")
		flag.PrintDefaults()
		return
	}

	if *tarOut != "" {
		if err := CreateTarball(*source, *tarOut); err != nil {
			log.Fatalf("create tar: %v", err)
		}
		fmt.Printf("  ✅ Tarball: %s → %s\n", *source, *tarOut)
		return
	}

	b := NewBuilder(*output, *verbose)

	if err := b.AddLayerFromDir(*source); err != nil {
		log.Fatalf("add layer: %v", err)
	}

	if err := b.Build(); err != nil {
		log.Fatalf("build: %v", err)
	}

	fmt.Printf("  ✅ Image built: %d layer(s) → %s\n", len(b.layers), *output)
}
