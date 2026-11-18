package memori

import (
	"os"
	"strings"
)

type OpenAIProvider struct {
	m *Memori
}

// Register wires an OpenAI-compatible client into Memori and returns Memori for
// chaining.
func (p *OpenAIProvider) Register(client *OpenAICompatClient) *Memori {
	if client == nil {
		client = NewOpenAIClient()
	}
	p.m.Config.mu.Lock()
	p.m.Config.LLM.Provider = "openai_compatible"
	p.m.Config.LLM.Version = "v1"
	p.m.Config.mu.Unlock()

	p.m.setOpenAIClient(client)
	return p.m
}

// Client returns the currently registered raw client, if any.
func (p *OpenAIProvider) Client() *OpenAICompatClient {
	return p.m.getOpenAIClient()
}

func NewOpenAIClient() *OpenAICompatClient {
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com"
	}
	return NewOpenAICompatClient(OpenAICompatOptions{
		BaseURL: base,
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})
}

func NewSiliconFlowClient() *OpenAICompatClient {
	base := strings.TrimSpace(os.Getenv("SILICONFLOW_BASE_URL"))
	if base == "" {
		base = "https://api.siliconflow.cn"
	}
	return NewOpenAICompatClient(OpenAICompatOptions{
		BaseURL: base,
		APIKey:  os.Getenv("SILICONFLOW_API_KEY"),
	})
}
