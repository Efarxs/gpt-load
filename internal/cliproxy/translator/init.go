package translator

import (
	_ "gpt-load/internal/cliproxy/translator/claude/gemini"
	_ "gpt-load/internal/cliproxy/translator/claude/openai/chat-completions"
	_ "gpt-load/internal/cliproxy/translator/claude/openai/responses"

	_ "gpt-load/internal/cliproxy/translator/gemini/claude"
	_ "gpt-load/internal/cliproxy/translator/gemini/gemini"
	_ "gpt-load/internal/cliproxy/translator/gemini/openai/chat-completions"
	_ "gpt-load/internal/cliproxy/translator/gemini/openai/responses"

	_ "gpt-load/internal/cliproxy/translator/openai/claude"
	_ "gpt-load/internal/cliproxy/translator/openai/gemini"
	_ "gpt-load/internal/cliproxy/translator/openai/openai/chat-completions"
	_ "gpt-load/internal/cliproxy/translator/openai/openai/responses"
)
