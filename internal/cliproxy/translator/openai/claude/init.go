package claude

import (
	. "gpt-load/internal/cliproxy/constant"
	"gpt-load/internal/cliproxy/interfaces"
	"gpt-load/internal/cliproxy/translator/translator"
)

func init() {
	translator.Register(
		Claude,
		OpenAI,
		ConvertClaudeRequestToOpenAI,
		interfaces.TranslateResponse{
			Stream:     ConvertOpenAIResponseToClaude,
			NonStream:  ConvertOpenAIResponseToClaudeNonStream,
			TokenCount: ClaudeTokenCount,
		},
	)
}
