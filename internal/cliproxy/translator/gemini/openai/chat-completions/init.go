package chat_completions

import (
	. "gpt-load/internal/cliproxy/constant"
	"gpt-load/internal/cliproxy/interfaces"
	"gpt-load/internal/cliproxy/translator/translator"
)

func init() {
	translator.Register(
		OpenAI,
		Gemini,
		ConvertOpenAIRequestToGemini,
		interfaces.TranslateResponse{
			Stream:    ConvertGeminiResponseToOpenAI,
			NonStream: ConvertGeminiResponseToOpenAINonStream,
		},
	)
}
