package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestServeDeckFromLocal(t *testing.T) {
	deckFile := writeDeckFile(t, []string{"2c", "ad"})
	outputDir := filepath.Join(t.TempDir(), "deck-layout")

	ctx := context.Background()
	if err := saveDeckLocal(ctx, outputDir, deckFile, "PNG-cards-1.3", "latest"); err != nil {
		t.Fatalf("saveDeckLocal failed: %v", err)
	}

	src, tag, err := openDeck(ctx, outputDir, false)
	if err != nil {
		t.Fatalf("openSource failed: %v", err)
	}
	if tag != "latest" {
		t.Errorf("tag = %q, want latest", tag)
	}

	ds, err := loadDeck(ctx, src, tag)
	if err != nil {
		t.Fatalf("loadDeck failed: %v", err)
	}

	if len(ds.cards) != 2 {
		t.Fatalf("got %d cards, want 2", len(ds.cards))
	}

	for _, shorthand := range ds.cards {
		filename, _ := shorthandToFilename(shorthand)
		if _, ok := ds.images[filename]; !ok {
			t.Errorf("missing image for %s (%s)", shorthand, filename)
		}
	}

	// Test HTTP handlers via httptest.
	mux := http.NewServeMux()
	mux.HandleFunc("/", ds.handleIndex)
	mux.HandleFunc("/images/", ds.handleImage)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("index status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "2c") {
		t.Error("index page should contain card shorthand '2c'")
	}

	resp2, err := http.Get(ts.URL + "/images/2_of_clubs.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("image status = %d, want 200", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}
}

func TestServeDeckFromRegistry(t *testing.T) {
	addr := setupRegistry(t)
	deckFile := writeDeckFile(t, []string{"2c", "ad"})
	ctx := context.Background()

	target := fmt.Sprintf("%s/deck:v1", addr)
	if err := pushDeck(ctx, target, deckFile, "PNG-cards-1.3", true); err != nil {
		t.Fatalf("pushDeck failed: %v", err)
	}

	src, tag, err := openDeck(ctx, target, true)
	if err != nil {
		t.Fatalf("openSource failed: %v", err)
	}
	if tag != "v1" {
		t.Errorf("tag = %q, want v1", tag)
	}

	ds, err := loadDeck(ctx, src, tag)
	if err != nil {
		t.Fatalf("loadDeck failed: %v", err)
	}

	if len(ds.cards) != 2 {
		t.Fatalf("got %d cards, want 2", len(ds.cards))
	}

	for _, shorthand := range ds.cards {
		filename, _ := shorthandToFilename(shorthand)
		if _, ok := ds.images[filename]; !ok {
			t.Errorf("missing image for %s (%s)", shorthand, filename)
		}
	}
}

func TestHandleImageNotFound(t *testing.T) {
	ds := &deckServer{
		cards:  []string{},
		images: map[string][]byte{},
	}

	req := httptest.NewRequest("GET", "/images/nonexistent.png", nil)
	w := httptest.NewRecorder()
	ds.handleImage(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestLoadDeckTamperedBlob(t *testing.T) {
	deckFile := writeDeckFile(t, []string{"2c"})
	outputDir := filepath.Join(t.TempDir(), "deck-layout")

	ctx := context.Background()
	if err := saveDeckLocal(ctx, outputDir, deckFile, "PNG-cards-1.3", "latest"); err != nil {
		t.Fatalf("saveDeckLocal failed: %v", err)
	}

	// Read index.json to find the manifest, then find a layer blob to tamper with.
	indexBytes, err := os.ReadFile(filepath.Join(outputDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index ocispec.Index
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		t.Fatal(err)
	}
	blobsDir := filepath.Join(outputDir, "blobs", "sha256")
	manifestBytes, err := os.ReadFile(filepath.Join(blobsDir, index.Manifests[0].Digest.Encoded()))
	if err != nil {
		t.Fatal(err)
	}
	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}

	// Overwrite the first layer blob with garbage.
	layerDigest := manifest.Layers[0].Digest.Encoded()
	blobPath := filepath.Join(blobsDir, layerDigest)
	if err := os.Chmod(blobPath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blobPath, []byte("tampered garbage data"), 0644); err != nil {
		t.Fatal(err)
	}

	src, tag, err := openDeck(ctx, outputDir, false)
	if err != nil {
		t.Fatalf("openSource failed: %v", err)
	}

	_, err = loadDeck(ctx, src, tag)
	if err == nil {
		t.Fatal("expected error when loading deck with tampered blob, got nil")
	}
}
