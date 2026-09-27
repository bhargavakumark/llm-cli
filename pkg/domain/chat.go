// Package domain holds the data types shared between the API client and the
// command layer.
package domain

// Chat roles as the OpenAI-compatible API names them.
const (
	RoleSystem = "system"
	RoleUser   = "user"
)

// Message is one turn in a chat request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Chunk is one streamed delta from a completion.
type Chunk struct {
	Content      string
	Reasoning    string
	FinishReason string
}

// Result is the assembled outcome of a completion.
type Result struct {
	Message Message
	Model   string
	// Chunks counts the streamed choices received, which is zero for a
	// non-streaming call.
	Chunks int
	// Usage is the token accounting the endpoint reported, if any.
	Usage Usage
}

// Usage is one endpoint's token accounting.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
	ReasoningTokens  int
}

// Reported says whether the endpoint sent any token accounting at all.
func (u Usage) Reported() bool {
	return u.PromptTokens != 0 || u.CompletionTokens != 0 || u.TotalTokens != 0
}
