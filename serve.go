package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

type deckServer struct {
	cards  []string
	images map[string][]byte
}

// openDeck opens a local OCI layout directory or a remote registry reference.
func openDeck(ctx context.Context, source string, plainHTTP bool) (oras.ReadOnlyTarget, string, error) {
	info, err := os.Stat(source)
	if err == nil && info.IsDir() {
		store, err := oci.NewWithContext(ctx, source)
		if err != nil {
			return nil, "", fmt.Errorf("opening OCI layout %s: %w", source, err)
		}
		return store, "latest", nil
	}

	tag := parseRef(source)
	repo, err := remote.NewRepository(source)
	if err != nil {
		return nil, "", fmt.Errorf("invalid source reference: %w", err)
	}
	repo.PlainHTTP = plainHTTP

	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("loading docker credentials: %w", err)
	}
	repo.Client = &auth.Client{
		Cache:      auth.NewCache(),
		Credential: credentials.Credential(credStore),
	}

	return repo, tag, nil
}

// loadDeck fetches the manifest, config, and image layers from an OCI source.
func loadDeck(ctx context.Context, src oras.ReadOnlyTarget, tag string) (*deckServer, error) {
	desc, err := src.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("resolving tag %q: %w", tag, err)
	}

	manifestBytes, err := content.FetchAll(ctx, src, desc)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshaling manifest: %w", err)
	}

	configBytes, err := content.FetchAll(ctx, src, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("fetching config: %w", err)
	}

	var cards []string
	if err := json.Unmarshal(configBytes, &cards); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	images := make(map[string][]byte)
	for _, layer := range manifest.Layers {
		if layer.MediaType != "image/png" {
			continue
		}
		filename := layer.Annotations[ocispec.AnnotationTitle]
		if filename == "" {
			continue
		}
		data, err := content.FetchAll(ctx, src, layer)
		if err != nil {
			return nil, fmt.Errorf("fetching layer %s: %w", filename, err)
		}
		images[filename] = data
	}

	return &deckServer{cards: cards, images: images}, nil
}

//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{
	"toFilename": func(s string) string {
		f, err := shorthandToFilename(s)
		if err != nil {
			return ""
		}
		return f
	},
}).Parse(indexHTML))

func (ds *deckServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexTmpl.Execute(w, struct{ Cards []string }{ds.cards})
}

func (ds *deckServer) handleImage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/images/")
	data, ok := ds.images[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(data)
}

// serveDeck loads a deck from a local OCI layout or remote registry and serves it over HTTP.
func serveDeck(ctx context.Context, source string, plainHTTP bool) error {
	src, tag, err := openDeck(ctx, source, plainHTTP)
	if err != nil {
		return err
	}

	ds, err := loadDeck(ctx, src, tag)
	if err != nil {
		return err
	}

	fmt.Printf("Serving %d cards on http://localhost:8080\n", len(ds.cards))
	http.HandleFunc("/", ds.handleIndex)
	http.HandleFunc("/images/", ds.handleImage)
	return http.ListenAndServe(":8080", nil)
}
