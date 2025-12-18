package memori

import (
	"os"
)

type OpenAIProvider struct {
	m *Memori
}

// Register wires an OpenAI-compatible client into Memori.
// This returns Memori to match the Python "mem.openai.register(client)" chaining style.
func (p *OpenAIProvider) Register(client *OpenAICompatClient) *Memori {
	// Default client if nil.
	if client == nil {
		client = NewOpenAICompatClient(OpenAICompatOptions{})
	}
	p.m.Config.mu.Lock()
	p.m.Config.LLM.Provider = "openai_compatible"
	p.m.Config.LLM.Version = "v1"
	p.m.Config.mu.Unlock()

	p.m.OpenAI = &OpenAIProvider{m: p.m}
	p.m.openAIClient = client
	return p.m
}

// Convenience constructors
func NewOpenAIClient() *OpenAICompatClient {
	return NewOpenAICompatClient(OpenAICompatOptions{
		BaseURL: "https://api.openai.com",
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})
}

func NewSiliconFlowClient() *OpenAICompatClient {
	base := os.Getenv("SILICONFLOW_BASE_URL")
	if base == "" {
		base = "https://api.siliconflow.cn"
	}
	return NewOpenAICompatClient(OpenAICompatOptions{
		BaseURL: base,
		APIKey:  os.Getenv("SILICONFLOW_API_KEY"),
	})
}


