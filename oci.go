package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

const (
	artifactType  = "application/vnd.card-deck"
	configMediaType = "application/vnd.card-deck.config+json"
)

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
// OCI store tagged with the given tag.
func buildDeck(ctx context.Context, deckPath, imagesDir, tag string) (*memory.Store, error) {
	cards, err := readDeck(deckPath)
	if err != nil {
		return nil, fmt.Errorf("reading deck: %w", err)
	}
	fmt.Printf("Deck %q: %d cards\n", deckPath, len(cards))

	store := memory.New()

	uniqueCards := make(map[string]bool)
	for _, c := range cards {
		uniqueCards[c] = true
	}

	var layers []v1.Descriptor
	for shorthand := range uniqueCards {
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
			"io.github.card-deck.card": shorthand,
		}

		layers = append(layers, desc)
		fmt.Printf("  prepared %s (%s, %d bytes)\n", shorthand, filename, len(data))
	}

	// Use the deck file as the manifest config.
	deckData, err := os.ReadFile(deckPath)
	if err != nil {
		return nil, fmt.Errorf("reading deck file for config: %w", err)
	}
	configDesc, err := oras.PushBytes(ctx, store, configMediaType, deckData)
	if err != nil {
		return nil, fmt.Errorf("pushing config: %w", err)
	}
	configDesc.Annotations = map[string]string{
		v1.AnnotationTitle: filepath.Base(deckPath),
	}

	packOpts := oras.PackManifestOptions{
		Layers:           layers,
		ConfigDescriptor: &configDesc,
	}
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, artifactType, packOpts)
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

	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return fmt.Errorf("loading docker credentials: %w", err)
	}
	ref.Client = &auth.Client{
		Cache:      auth.NewCache(),
		Credential: credentials.Credential(credStore),
	}

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
