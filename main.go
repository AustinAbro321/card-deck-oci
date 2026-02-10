package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
)

var suits = map[byte]string{
	'c': "clubs",
	'd': "diamonds",
	's': "spades",
	'h': "hearts",
}

var ranks = map[string]string{
	"2": "2", "3": "3", "4": "4", "5": "5",
	"6": "6", "7": "7", "8": "8", "9": "9", "10": "10",
	"j": "jack", "q": "queen", "k": "king", "a": "ace",
}

// shorthandToFilename converts e.g. "2c" -> "2_of_clubs.png", "ad" -> "ace_of_diamonds.png"
func shorthandToFilename(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) < 2 {
		return "", fmt.Errorf("invalid card shorthand: %q", s)
	}

	suitChar := s[len(s)-1]
	rankStr := s[:len(s)-1]

	suit, ok := suits[suitChar]
	if !ok {
		return "", fmt.Errorf("unknown suit %q in %q", string(suitChar), s)
	}

	rank, ok := ranks[rankStr]
	if !ok {
		return "", fmt.Errorf("unknown rank %q in %q", rankStr, s)
	}

	return fmt.Sprintf("%s_of_%s.png", rank, suit), nil
}

// readDeck reads card shorthands from a file, one per line, skipping blanks and # comments.
func readDeck(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cards []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cards = append(cards, line)
	}
	return cards, scanner.Err()
}

// parseTag extracts the tag from a registry reference like "localhost:5000/repo:tag".
func parseTag(target string) string {
	parts := strings.SplitN(target, ":", 3)
	if len(parts) == 3 {
		return parts[2]
	}
	if len(parts) == 2 && !strings.Contains(parts[1], "/") {
		return parts[1]
	}
	return "latest"
}

// buildDeck reads the deck file, loads card PNGs, and packs them into an in-memory
// OCI store. Returns the store, the manifest tag, and any error.
func buildDeck(ctx context.Context, deckPath, imagesDir, tag string) (*memory.Store, error) {
	cards, err := readDeck(deckPath)
	if err != nil {
		return nil, fmt.Errorf("reading deck: %w", err)
	}
	fmt.Printf("Deck %q: %d cards\n", deckPath, len(cards))

	store := memory.New()

	var layers []v1.Descriptor
	for _, shorthand := range cards {
		filename, err := shorthandToFilename(shorthand)
		if err != nil {
			return nil, err
		}

		data, err := os.ReadFile(filepath.Join(imagesDir, filename))
		if err != nil {
			return nil, fmt.Errorf("reading card image %s: %w", filename, err)
		}

		desc, err := oras.PushBytes(ctx, store, "image/png", data)
		if err != nil {
			return nil, fmt.Errorf("pushing layer %s: %w", shorthand, err)
		}

		desc.Annotations = map[string]string{
			v1.AnnotationTitle:   filename,
			"vnd.card-deck.card": shorthand,
		}

		layers = append(layers, desc)
		fmt.Printf("  prepared %s (%s, %d bytes)\n", shorthand, filename, len(data))
	}

	packOpts := oras.PackManifestOptions{
		Layers: layers,
	}
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.card-deck", packOpts)
	if err != nil {
		return nil, fmt.Errorf("packing manifest: %w", err)
	}

	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return nil, fmt.Errorf("tagging manifest: %w", err)
	}

	return store, nil
}

// pushDeck builds an OCI artifact from a deck of cards and pushes it to a registry.
func pushDeck(ctx context.Context, target, deckPath, imagesDir string, plainHTTP bool) error {
	tag := parseTag(target)

	store, err := buildDeck(ctx, deckPath, imagesDir, tag)
	if err != nil {
		return err
	}

	ref, err := remote.NewRepository(target)
	if err != nil {
		return fmt.Errorf("invalid target reference: %w", err)
	}
	ref.PlainHTTP = plainHTTP

	copyOpts := oras.CopyOptions{}
	copyOpts.PreCopy = func(_ context.Context, desc v1.Descriptor) error {
		if desc.MediaType == "image/png" {
			name := desc.Annotations[v1.AnnotationTitle]
			fmt.Printf("  uploading %s (%d bytes)\n", name, desc.Size)
		}
		return nil
	}
	copyOpts.OnCopySkipped = func(_ context.Context, desc v1.Descriptor) error {
		if desc.MediaType == "image/png" {
			name := desc.Annotations[v1.AnnotationTitle]
			fmt.Printf("  skipped %s (already exists)\n", name)
		}
		return nil
	}

	fmt.Printf("\nPushing to %s ...\n", target)
	_, err = oras.Copy(ctx, store, tag, ref, tag, copyOpts)
	if err != nil {
		return fmt.Errorf("copying to registry: %w", err)
	}

	fmt.Println("Done.")
	return nil
}

// saveDeckLocal builds an OCI artifact and writes it to a local OCI layout directory.
func saveDeckLocal(ctx context.Context, outputDir, deckPath, imagesDir, tag string) error {
	store, err := buildDeck(ctx, deckPath, imagesDir, tag)
	if err != nil {
		return err
	}

	dst, err := oci.New(outputDir)
	if err != nil {
		return fmt.Errorf("creating OCI layout at %s: %w", outputDir, err)
	}

	copyOpts := oras.CopyOptions{}
	copyOpts.PreCopy = func(_ context.Context, desc v1.Descriptor) error {
		if desc.MediaType == "image/png" {
			name := desc.Annotations[v1.AnnotationTitle]
			fmt.Printf("  writing %s (%d bytes)\n", name, desc.Size)
		}
		return nil
	}

	fmt.Printf("\nSaving to %s ...\n", outputDir)
	_, err = oras.Copy(ctx, store, tag, dst, tag, copyOpts)
	if err != nil {
		return fmt.Errorf("copying to OCI layout: %w", err)
	}

	fmt.Println("Done.")
	return nil
}

func run() error {
	target := flag.String("target", "", "registry reference (e.g. localhost:5000/deck:v1)")
	local := flag.String("local", "", "output OCI layout directory (instead of pushing to registry)")
	deck := flag.String("deck", "cards.txt", "path to deck definition file")
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
