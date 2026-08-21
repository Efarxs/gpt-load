package registry

import "strings"

type ModelInfo struct {
	ID                        string
	Object                    string
	Created                   int64
	OwnedBy                   string
	Type                      string
	DisplayName               string
	Name                      string
	Version                   string
	Description               string
	InputTokenLimit           int
	OutputTokenLimit          int
	SupportedGenerationMethods []string
	ContextLength             int
	MaxCompletionTokens       int
	SupportedParameters       []string
	SupportedInputModalities  []string
	SupportedOutputModalities []string
	SupportsWebSearch         bool
	Thinking                  *ThinkingSupport
	UserDefined               bool
}

type ThinkingSupport struct {
	Min            int
	Max            int
	ZeroAllowed    bool
	DynamicAllowed bool
	Levels         []string
}

func LookupModelInfo(modelID string, provider ...string) *ModelInfo {
	_ = provider
	_ = strings.TrimSpace(modelID)
	return nil
}

func LookupStaticModelInfo(modelID string) *ModelInfo {
	_ = modelID
	return nil
}
