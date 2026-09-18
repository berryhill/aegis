package localinference

type openAIChatRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	MaxTokens           int      `json:"max_tokens,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	Stop                any      `json:"stop,omitempty"`
	Tools               []any    `json:"tools,omitempty"`
	ToolChoice          any      `json:"tool_choice,omitempty"`
	ResponseFormat      any      `json:"response_format,omitempty"`
	ReasoningEffort     string   `json:"reasoning_effort,omitempty"`
	User                string   `json:"user,omitempty"`
	SessionID           string   `json:"session_id,omitempty"`
}
type openAIMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  []any  `json:"tool_calls,omitempty"`
}
type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
		Logprobs     any           `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
