package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds endpoint + model settings for the test run.
type Config struct {
	BaseURL string   `yaml:"base_url"`
	Models  []string `yaml:"models"`
	APIKey  string   `yaml:"api_key"`
	// Benchmark settings (used when --mode benchmark).
	Benchmark BenchmarkConfig `yaml:"benchmark,omitempty"`
}

// BenchmarkConfig holds benchmark-mode parameters.
type BenchmarkConfig struct {
	Iterations  int    `yaml:"iterations,omitempty"`
	Concurrency int    `yaml:"concurrency,omitempty"`
	Prompt      string `yaml:"prompt,omitempty"`
}

// PromptPong and PromptLong are the valid prompt styles for benchmark mode.
const (
	PromptPong = "pong"
	PromptLong = "long"
)

// Load reads config.yaml (if present) and applies env overrides.
// Env wins over file for the values it sets:
//
//	OPENAI_BASE_URL -> BaseURL
//	OPENAI_MODEL    -> Models (comma-separated; a single value is fine)
//	OPENAI_API_KEY  -> APIKey
func Load(path string) (*Config, error) {
	cfg := &Config{}

	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("OPENAI_MODEL"); v != "" {
		cfg.Models = splitModels(v)
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.APIKey = v
	}

	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required (config.yaml or OPENAI_BASE_URL)")
	}

	// Normalize BaseURL: strip trailing slashes, then ensure it ends with /v1
	// so all clients can append just their endpoint-specific path (e.g.
	// "/responses", "/chat/completions", "/messages").
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(cfg.BaseURL, "/v1") {
		cfg.BaseURL += "/v1"
	}
	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("models is required (config.yaml or OPENAI_MODEL)")
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("api_key is required (config.yaml or OPENAI_API_KEY)")
	}

	// Apply defaults for benchmark config.
	if cfg.Benchmark.Iterations <= 0 {
		cfg.Benchmark.Iterations = 10
	}
	if cfg.Benchmark.Concurrency <= 0 {
		cfg.Benchmark.Concurrency = 5
	}
	if cfg.Benchmark.Prompt == "" {
		cfg.Benchmark.Prompt = PromptPong
	}

	return cfg, nil
}

// splitModels splits a comma-separated model list, trimming whitespace and
// dropping empty entries.
func splitModels(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
