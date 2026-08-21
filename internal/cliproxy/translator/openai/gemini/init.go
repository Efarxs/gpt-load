package gemini

import (
	. "gpt-load/internal/cliproxy/constant"
	"gpt-load/internal/cliproxy/interfaces"
	"gpt-load/internal/cliproxy/translator/translator"
)

func init() {
	translator.Register(
		Gemini,
		OpenAI,
		ConvertGeminiRequestToOpenAI,
		interfaces.TranslateResponse{
			Stream:     ConvertOpenAIResponseToGemini,
			NonStream:  ConvertOpenAIResponseToGeminiNonStream,
			TokenCount: GeminiTokenCount,
		},
	)
}
