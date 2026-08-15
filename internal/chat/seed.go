package chat

import (
	"context"

	"llm-api-test/internal/cases"
	"llm-api-test/internal/registry"
)

// SeedCase verifies that the endpoint accepts the seed parameter.
type SeedCase struct {
	client *Client
}

func (c *SeedCase) ID() string   { return "chat:seed" }
func (c *SeedCase) Name() string { return "seed" }
func (c *SeedCase) Desc() string { return "POST /v1/chat/completions accepts `seed`" }

func (c *SeedCase) Run(ctx context.Context, model string) *registry.CompatResult {
	seed := 42
	req := &Request{
		Model:    model,
		Messages: []Message{{Role: "user", Content: "Reply with exactly the word: pong"}},
		Seed:     &seed,
	}
	res, err := c.client.Send(ctx, req)
	if err != nil {
		return cases.Fail("request failed: %v", err)
	}
	if res.Content == "" {
		return cases.FailRaw(res.Raw, "no assistant output")
	}
	return cases.Pass("seed accepted", res.Raw)
}
