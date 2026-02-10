package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/olareg/olareg"
	"github.com/olareg/olareg/config"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
)

// setupRegistry starts an in-memory olareg registry and returns its host:port address.
func setupRegistry(t *testing.T) string {
	t.Helper()
	regHandler := olareg.New(config.Config{
		Storage: config.ConfigStorage{
			StoreType: config.StoreMem,
		},
	})
	ts := httptest.NewServer(regHandler)
	t.Cleanup(func() {
		ts.Close()
		regHandler.Close()
	})
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func writeDeckFile(t *testing.T, cards []string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "deck-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		fmt.Fprintln(f, c)
	}
	f.Close()
	return f.Name()
}

func TestShorthandToFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"2c", "2_of_clubs.png", false},
		{"2d", "2_of_diamonds.png", false},
		{"10h", "10_of_hearts.png", false},
		{"ad", "ace_of_diamonds.png", false},
		{"ks", "king_of_spades.png", false},
		{"qh", "queen_of_hearts.png", false},
		{"jc", "jack_of_clubs.png", false},
		{"3s", "3_of_spades.png", false},
		// Case insensitive
		{"AD", "ace_of_diamonds.png", false},
		{"Ks", "king_of_spades.png", false},
		// Errors
		{"", "", true},
		{"x", "", true},
		{"2x", "", true},
		{"xc", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := shorthandToFilename(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if got != tc.expected {
				t.Errorf("shorthandToFilename(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseTag(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"localhost:5000/deck:v1", "v1"},
		{"localhost:5000/deck:latest", "latest"},
		{"myregistry.io/ns/repo:v2", "v2"},
		{"localhost:5000/deck", "latest"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := parseTag(tc.input)
			if got != tc.want {
				t.Errorf("parseTag(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestReadDeck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deck.txt")
	content := "# comment\n2c\n\nad\n# another comment\nkh\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cards, err := readDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"2c", "ad", "kh"}
	if len(cards) != len(expected) {
		t.Fatalf("got %d cards, want %d", len(cards), len(expected))
	}
	for i := range expected {
		if cards[i] != expected[i] {
			t.Errorf("card[%d] = %q, want %q", i, cards[i], expected[i])
		}
	}
}

func TestReadDeckMissingFile(t *testing.T) {
	_, err := readDeck("/nonexistent/deck.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPushDeck(t *testing.T) {
	addr := setupRegistry(t)
	deckFile := writeDeckFile(t, []string{"2c", "ad"})

	ctx := context.Background()
	target := fmt.Sprintf("%s/deck:v1", addr)

	err := pushDeck(ctx, target, deckFile, "PNG-cards-1.3", true)
	if err != nil {
		t.Fatalf("pushDeck failed: %v", err)
	}

	// Verify the manifest exists in the registry.
	repo, err := remote.NewRepository(target)
	if err != nil {
		t.Fatal(err)
	}
	repo.PlainHTTP = true

	desc, err := oras.Resolve(ctx, repo, "v1", oras.DefaultResolveOptions)
	if err != nil {
		t.Fatalf("failed to resolve pushed manifest: %v", err)
	}
	if desc.Digest.String() == "" {
		t.Fatal("resolved descriptor has empty digest")
	}

	// Fetch the manifest and verify layers.
	_, manifestBytes, err := oras.FetchBytes(ctx, repo, "v1", oras.DefaultFetchBytesOptions)
	if err != nil {
		t.Fatalf("failed to fetch manifest: %v", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("failed to unmarshal manifest: %v", err)
	}

	if len(manifest.Layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(manifest.Layers))
	}

	expectedCards := []struct {
		shorthand string
		filename  string
	}{
		{"2c", "2_of_clubs.png"},
		{"ad", "ace_of_diamonds.png"},
	}
	for i, ec := range expectedCards {
		layer := manifest.Layers[i]
		if layer.MediaType != "image/png" {
			t.Errorf("layer %d media type = %q, want image/png", i, layer.MediaType)
		}
		if layer.Annotations[ocispec.AnnotationTitle] != ec.filename {
			t.Errorf("layer %d title = %q, want %q", i, layer.Annotations[ocispec.AnnotationTitle], ec.filename)
		}
		if layer.Annotations["vnd.card-deck.card"] != ec.shorthand {
			t.Errorf("layer %d card = %q, want %q", i, layer.Annotations["vnd.card-deck.card"], ec.shorthand)
		}
	}

	if manifest.ArtifactType != "application/vnd.card-deck" {
		t.Errorf("artifact type = %q, want application/vnd.card-deck", manifest.ArtifactType)
	}
}

func TestPushDeckDeduplication(t *testing.T) {
	addr := setupRegistry(t)
	ctx := context.Background()

	// Push first deck with 2c and ad.
	deck1 := writeDeckFile(t, []string{"2c", "ad"})
	target1 := fmt.Sprintf("%s/deck:v1", addr)
	if err := pushDeck(ctx, target1, deck1, "PNG-cards-1.3", true); err != nil {
		t.Fatalf("pushDeck v1 failed: %v", err)
	}

	// Build second deck sharing 2c but adding kh.
	// Replicate the OCI workflow inline so we can attach custom copy callbacks.
	deck2 := writeDeckFile(t, []string{"2c", "kh"})
	cards2, err := readDeck(deck2)
	if err != nil {
		t.Fatal(err)
	}

	store := memory.New()
	var layers []ocispec.Descriptor
	for _, shorthand := range cards2 {
		filename, err := shorthandToFilename(shorthand)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join("PNG-cards-1.3", filename))
		if err != nil {
			t.Fatal(err)
		}
		desc, err := oras.PushBytes(ctx, store, "image/png", data)
		if err != nil {
			t.Fatal(err)
		}
		desc.Annotations = map[string]string{
			ocispec.AnnotationTitle: filename,
			"vnd.card-deck.card":   shorthand,
		}
		layers = append(layers, desc)
	}

	packOpts := oras.PackManifestOptions{Layers: layers}
	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, "application/vnd.card-deck", packOpts)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Tag(ctx, manifestDesc, "v2"); err != nil {
		t.Fatal(err)
	}

	target2 := fmt.Sprintf("%s/deck:v2", addr)
	ref, err := remote.NewRepository(target2)
	if err != nil {
		t.Fatal(err)
	}
	ref.PlainHTTP = true

	var uploaded, skipped []string
	copyOpts := oras.CopyOptions{}
	copyOpts.PreCopy = func(_ context.Context, desc ocispec.Descriptor) error {
		if desc.MediaType == "image/png" {
			uploaded = append(uploaded, desc.Annotations["vnd.card-deck.card"])
		}
		return nil
	}
	copyOpts.OnCopySkipped = func(_ context.Context, desc ocispec.Descriptor) error {
		if desc.MediaType == "image/png" {
			skipped = append(skipped, desc.Annotations["vnd.card-deck.card"])
		}
		return nil
	}

	_, err = oras.Copy(ctx, store, "v2", ref, "v2", copyOpts)
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	// 2c should be skipped (already exists from v1), kh should be uploaded.
	if len(skipped) != 1 || skipped[0] != "2c" {
		t.Errorf("expected skipped=[2c], got %v", skipped)
	}
	if len(uploaded) != 1 || uploaded[0] != "kh" {
		t.Errorf("expected uploaded=[kh], got %v", uploaded)
	}
}

func TestSaveDeckLocal(t *testing.T) {
	deckFile := writeDeckFile(t, []string{"2c", "ad"})
	outputDir := filepath.Join(t.TempDir(), "deck-layout")

	ctx := context.Background()
	err := saveDeckLocal(ctx, outputDir, deckFile, "PNG-cards-1.3", "v1")
	if err != nil {
		t.Fatalf("saveDeckLocal failed: %v", err)
	}

	// Verify OCI layout structure exists.
	layoutFile := filepath.Join(outputDir, "oci-layout")
	if _, err := os.Stat(layoutFile); err != nil {
		t.Fatalf("oci-layout file missing: %v", err)
	}
	indexFile := filepath.Join(outputDir, "index.json")
	if _, err := os.Stat(indexFile); err != nil {
		t.Fatalf("index.json file missing: %v", err)
	}
	blobsDir := filepath.Join(outputDir, "blobs", "sha256")
	if _, err := os.Stat(blobsDir); err != nil {
		t.Fatalf("blobs/sha256 directory missing: %v", err)
	}

	// Read index.json and verify it references our manifest.
	indexBytes, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatal(err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		t.Fatalf("failed to unmarshal index.json: %v", err)
	}
	if len(index.Manifests) != 1 {
		t.Fatalf("expected 1 manifest in index, got %d", len(index.Manifests))
	}

	// Read the manifest blob and verify layers.
	manifestDigest := index.Manifests[0].Digest
	manifestPath := filepath.Join(blobsDir, manifestDigest.Encoded())
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read manifest blob: %v", err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("failed to unmarshal manifest: %v", err)
	}

	if len(manifest.Layers) != 2 {
		t.Fatalf("expected 2 layers, got %d", len(manifest.Layers))
	}
	if manifest.ArtifactType != "application/vnd.card-deck" {
		t.Errorf("artifact type = %q, want application/vnd.card-deck", manifest.ArtifactType)
	}

	// Verify each layer blob exists on disk.
	for i, layer := range manifest.Layers {
		blobPath := filepath.Join(blobsDir, layer.Digest.Encoded())
		info, err := os.Stat(blobPath)
		if err != nil {
			t.Errorf("layer %d blob missing: %v", i, err)
			continue
		}
		if info.Size() != layer.Size {
			t.Errorf("layer %d size = %d, want %d", i, info.Size(), layer.Size)
		}
	}
}

func TestSaveDeckLocalBadDeck(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "deck-layout")
	err := saveDeckLocal(context.Background(), outputDir, "/nonexistent/deck.txt", "PNG-cards-1.3", "v1")
	if err == nil {
		t.Fatal("expected error for missing deck file")
	}
}

func TestPushDeckBadDeckFile(t *testing.T) {
	addr := setupRegistry(t)
	target := fmt.Sprintf("%s/deck:v1", addr)
	err := pushDeck(context.Background(), target, "/nonexistent/deck.txt", "PNG-cards-1.3", true)
	if err == nil {
		t.Fatal("expected error for missing deck file")
	}
}

func TestPushDeckBadCard(t *testing.T) {
	addr := setupRegistry(t)
	deckFile := writeDeckFile(t, []string{"zz"})
	target := fmt.Sprintf("%s/deck:v1", addr)
	err := pushDeck(context.Background(), target, deckFile, "PNG-cards-1.3", true)
	if err == nil {
		t.Fatal("expected error for invalid card shorthand")
	}
}

func TestPushDeckMissingImage(t *testing.T) {
	addr := setupRegistry(t)
	deckFile := writeDeckFile(t, []string{"2c"})
	target := fmt.Sprintf("%s/deck:v1", addr)
	err := pushDeck(context.Background(), target, deckFile, "/nonexistent/images", true)
	if err == nil {
		t.Fatal("expected error for missing image directory")
	}
}
