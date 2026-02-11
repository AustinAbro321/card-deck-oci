package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeDeckFromLocal(t *testing.T) {
	deckFile := writeDeckFile(t, []string{"2c", "ad"})
	outputDir := filepath.Join(t.TempDir(), "deck-layout")

	ctx := context.Background()
	if err := saveDeckLocal(ctx, outputDir, deckFile, "PNG-cards-1.3", "latest"); err != nil {
		t.Fatalf("saveDeckLocal failed: %v", err)
	}

	src, tag, err := openSource(ctx, outputDir, false)
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

	src, tag, err := openSource(ctx, target, true)
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
