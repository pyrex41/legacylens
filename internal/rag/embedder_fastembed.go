package rag

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	fastembed "github.com/grumpylabs/fastembed-go"
	ort "github.com/yalue/onnxruntime_go"
)

// FastEmbedder provides local on-device embeddings using ONNX Runtime
// via fastembed-go. Model: all-MiniLM-L6-v2 (384 dimensions).
type FastEmbedder struct {
	model *fastembed.FlagEmbedding
	dim   int
	mu    sync.Mutex
}

// NewFastEmbedder creates a local embedder with all-MiniLM-L6-v2.
// The ONNX model is auto-downloaded on first use.
func NewFastEmbedder(cacheDir string) (*FastEmbedder, error) {
	if cacheDir == "" {
		cacheDir = "local_cache"
	}

	// Auto-detect ONNX Runtime library if not already set
	if !ort.IsInitialized() {
		if libPath := findONNXLib(); libPath != "" {
			ort.SetSharedLibraryPath(libPath)
		}
	}

	showProgress := true
	model, err := fastembed.NewFlagEmbedding(&fastembed.InitOptions{
		Model:                fastembed.AllMiniLML6V2,
		CacheDir:             cacheDir,
		ShowDownloadProgress: &showProgress,
	})
	if err != nil {
		// fastembed expects model_optimized.onnx but the archive ships model.onnx.
		// Create symlink and retry.
		modelDir := filepath.Join(cacheDir, "fast-all-MiniLM-L6-v2")
		optimized := filepath.Join(modelDir, "model_optimized.onnx")
		original := filepath.Join(modelDir, "model.onnx")
		if _, statErr := os.Stat(original); statErr == nil {
			if _, statErr := os.Stat(optimized); statErr != nil {
				os.Symlink("model.onnx", optimized)
				model, err = fastembed.NewFlagEmbedding(&fastembed.InitOptions{
					Model:                fastembed.AllMiniLML6V2,
					CacheDir:             cacheDir,
					ShowDownloadProgress: &showProgress,
				})
			}
		}
		if err != nil {
			return nil, fmt.Errorf("init fastembed: %w", err)
		}
	}
	return &FastEmbedder{model: model, dim: 384}, nil
}

// findONNXLib locates the ONNX Runtime shared library on the system.
func findONNXLib() string {
	// Check ONNX_PATH env first
	if p := os.Getenv("ONNX_PATH"); p != "" {
		return p
	}

	var libName string
	switch runtime.GOOS {
	case "darwin":
		libName = "libonnxruntime.dylib"
	case "linux":
		libName = "libonnxruntime.so"
	default:
		return ""
	}

	// Common paths
	candidates := []string{
		filepath.Join("/opt/homebrew/lib", libName),
		filepath.Join("/usr/local/lib", libName),
		filepath.Join("/usr/lib", libName),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	// Try brew --prefix as fallback
	if out, err := exec.Command("brew", "--prefix", "onnxruntime").Output(); err == nil {
		p := filepath.Join(strings.TrimSpace(string(out)), "lib", libName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

func (e *FastEmbedder) Dimension() int { return e.dim }

// maxEmbedChars is the maximum text length to send to the tokenizer.
// all-MiniLM-L6-v2 has a 512 token limit; ~4 chars/token → 1500 chars is safe.
const maxEmbedChars = 1500

func (e *FastEmbedder) Embed(text string) []float32 {
	if text == "" {
		return make([]float32, e.dim)
	}

	// Sanitize: replace non-printable/unusual chars that crash the tokenizer
	text = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return ' '
		}
		return r
	}, text)

	// Truncate to avoid tokenizer issues on very long inputs
	if len(text) > maxEmbedChars {
		text = text[:maxEmbedChars]
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return make([]float32, e.dim)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Use a subprocess-like approach: embed in a way that panics are caught.
	// Since EncodeBatch spawns goroutines we can't recover from,
	// we use QueryEmbed which processes a single string.
	vec, err := e.model.QueryEmbed(text)
	if err != nil {
		return make([]float32, e.dim)
	}
	if len(vec) == 0 {
		return make([]float32, e.dim)
	}
	return vec
}

// Close releases ONNX Runtime resources.
func (e *FastEmbedder) Close() error {
	if e.model != nil {
		return e.model.Destroy()
	}
	return nil
}
