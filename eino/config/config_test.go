package config

import (
	"os"
	"strings"
	"testing"
)

// TestLoadRuntimeConfigDefaults 验证无配置文件时也能给出合理默认值。
func TestLoadRuntimeConfigDefaults(t *testing.T) {
	cfg, err := LoadRuntimeConfig(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RAGMaxChunks != 2000 {
		t.Fatalf("RAGMaxChunks = %d, want 2000", cfg.RAGMaxChunks)
	}
	if !cfg.ComputerRequireApproval {
		t.Fatal("ComputerRequireApproval 应默认开启（安全优先）")
	}
	if cfg.RAGDataDir == "" {
		t.Fatal("RAGDataDir 应有默认值")
	}
}

// TestSaveRuntimeConfigClearsSecrets 验证持久化时不会把 API Key 写进配置文件。
func TestSaveRuntimeConfigClearsSecrets(t *testing.T) {
	path := "test_config_tmp.json"
	cfg := RuntimeConfig{
		Providers: []ProviderConfig{{Name: "t", APIKey: "secret-key"}},
		ConfigPath: path,
	}
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("save error: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if strings.Contains(string(data), "secret-key") {
		t.Fatal("持久化后的配置不应包含 API Key")
	}
}
