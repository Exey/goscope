package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	ExcludePaths    []string `json:"excludePaths"`
	MaxFilesAnalyze int      `json:"maxFilesAnalyze"`
	GitCommitLimit  int      `json:"gitCommitLimit"`
	EnableCache     bool     `json:"enableCache"`
	EnableParallel  bool     `json:"enableParallel"`
	HotspotCount    int      `json:"hotspotCount"`
	FileExtensions  []string `json:"fileExtensions"`
}

const DefaultConfigPath = ".goscope.json"

func DefaultConfig() Config {
	return Config{
		ExcludePaths: []string{
			".git", ".build", "node_modules", "vendor", "dist",
			"build", ".idea", ".vscode", "__pycache__", ".cache",
			"DerivedData", "Pods", "target",
		},
		MaxFilesAnalyze: 50000,
		GitCommitLimit:  1000,
		EnableCache:     false,
		EnableParallel:  true,
		HotspotCount:    15,
		FileExtensions:  []string{"go", "proto"},
	}
}

func Load(path string) Config {
	if path == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig()
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Failed to parse config, using defaults: %v\n", err)
		return DefaultConfig()
	}
	return cfg
}

func CreateDefault(path string) error {
	if path == "" {
		path = DefaultConfigPath
	}
	cfg := DefaultConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Created default config at %s\n", path)
	return nil
}
