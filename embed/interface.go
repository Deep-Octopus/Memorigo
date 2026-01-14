package embed

import (
	"context"
	"errors"
)

var (
	ErrEmbeddingServiceUnavailable = errors.New("embedding service unavailable")
	ErrEmbeddingDimensionMismatch  = errors.New("embedding dimension mismatch")
)

// Embedder 定义统一的嵌入接口
type Embedder interface {
	// EmbedText 将文本转换为向量嵌入
	EmbedText(ctx context.Context, text string) ([]float32, error)

	// EmbedTexts 批量将多个文本转换为向量嵌入
	EmbedTexts(ctx context.Context, texts []string) ([][]float32, error)

	// Dimension 返回嵌入向量的维度
	Dimension() int

	// Provider 返回提供商名称
	Provider() string
}

// Config 嵌入配置
type Config struct {
	Provider  string // "openai", "siliconflow", "hash" (fallback)
	APIKey    string
	BaseURL   string
	Model     string
	Dimension int
}

// NewEmbedder 根据配置创建嵌入器
func NewEmbedder(config Config) Embedder {
	switch config.Provider {
	case "openai":
		return NewOpenAIEmbedder(config)
	case "siliconflow":
		return NewSiliconFlowEmbedder(config)
	case "hash":
		fallthrough
	default:
		// fallback to simple hash embedding for offline/demo use
		return NewHashEmbedder()
	}
}

// cosineSimilarity 计算两个向量的余弦相似度
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (normA * normB)
}
