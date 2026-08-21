package responses

import (
	. "gpt-load/internal/cliproxy/constant"
	"gpt-load/internal/cliproxy/interfaces"
	"gpt-load/internal/cliproxy/translator/translator"
)

func init() {
	translator.Register(
		OpenaiResponse,
		Gemini,
		ConvertOpenAIResponsesRequestToGemini,
		interfaces.TranslateResponse{
			Stream:    ConvertGeminiResponseToOpenAIResponses,
			NonStream: ConvertGeminiResponseToOpenAIResponsesNonStream,
		},
	)
}
