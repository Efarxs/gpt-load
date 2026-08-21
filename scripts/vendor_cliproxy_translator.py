# 从 CLIProxyAPI 完整拷贝协议转换器，只改 import 前缀，不删减转换逻辑。
from __future__ import annotations

import shutil
from pathlib import Path

SRC = Path(r"D:\code\go\CLIProxyAPI")
DST = Path(r"D:\code\go\gpt-load\internal\cliproxy")
# Go 禁止外部包导入 .../internal/cliproxy/internal/...，因此摊平目录。
REPLACES = (
    ("github.com/router-for-me/CLIProxyAPI/v7/internal/", "gpt-load/internal/cliproxy/"),
    ("github.com/router-for-me/CLIProxyAPI/v7/sdk/", "gpt-load/internal/cliproxy/sdk/"),
)

TRANSLATOR_KEEP_PREFIXES = (
    "claude/",
    "openai/",
    "gemini/",
    "common/",
    "translator/",
    "init.go",
)

UTIL_FILES = (
    "translator.go",
    "claude_attribution.go",
    "claude_tool_result.go",
    "claude_tool_id.go",
    "claude_model.go",
    "gemini_schema.go",
    "image.go",
)

THINKING_FILES = (
    "apply.go",
    "convert.go",
    "errors.go",
    "strip.go",
    "suffix.go",
    "text.go",
    "types.go",
    "validate.go",
)


def rewrite_text(text: str) -> str:
    for old, new in REPLACES:
        text = text.replace(old, new)
    return text


def copy_file(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    data = src.read_bytes()
    if src.suffix == ".go":
        text = data.decode("utf-8")
        dst.write_text(rewrite_text(text), encoding="utf-8", newline="\n")
    else:
        dst.write_bytes(data)


def copy_tree(src: Path, dst: Path, predicate) -> None:
    for path in src.rglob("*"):
        if not path.is_file():
            continue
        rel = path.relative_to(src).as_posix()
        if not predicate(rel):
            continue
        copy_file(path, dst / path.relative_to(src))


def main() -> None:
    if DST.exists():
        shutil.rmtree(DST)

    copy_tree(
        SRC / "internal" / "translator",
        DST / "translator",
        lambda rel: (
            not rel.endswith("_test.go")
            and not rel.startswith("antigravity/")
            and not rel.startswith("codex/")
            and (rel == "init.go" or rel.startswith(TRANSLATOR_KEEP_PREFIXES))
        ),
    )
    copy_tree(
        SRC / "sdk" / "translator",
        DST / "sdk" / "translator",
        lambda rel: not rel.endswith("_test.go") and not rel.startswith("builtin/"),
    )
    copy_tree(
        SRC / "internal" / "constant",
        DST / "constant",
        lambda rel: rel.endswith(".go") and not rel.endswith("_test.go"),
    )
    copy_tree(
        SRC / "internal" / "interfaces",
        DST / "interfaces",
        lambda rel: rel.endswith(".go") and not rel.endswith("_test.go"),
    )
    copy_tree(
        SRC / "internal" / "signature",
        DST / "signature",
        lambda rel: rel.endswith(".go") and not rel.endswith("_test.go"),
    )
    copy_tree(
        SRC / "internal" / "translator" / "common",
        DST / "translator" / "common",
        lambda rel: rel.endswith(".go") and not rel.endswith("_test.go"),
    )

    for name in THINKING_FILES:
        src = SRC / "internal" / "thinking" / name
        if src.exists():
            copy_file(src, DST / "thinking" / name)

    for name in UTIL_FILES:
        src = SRC / "internal" / "util" / name
        if src.exists():
            copy_file(src, DST / "util" / name)

    copy_file(SRC / "internal" / "misc" / "mime-type.go", DST / "misc" / "mime-type.go")

    # SanitizeFunctionName 单独抽出，避免拉入 CLIProxyAPI config。
    (DST / "util").mkdir(parents=True, exist_ok=True)
    (DST / "util" / "sanitize_function.go").write_text(
        '''package util

import (
	"regexp"
	"strings"
)

var functionNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.:-]`)

func SanitizeFunctionName(name string) string {
	if name == "" {
		return ""
	}
	sanitized := functionNameSanitizer.ReplaceAllString(name, "_")
	if sanitized == "" {
		return "_"
	}
	first := sanitized[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') && first != '_' {
		sanitized = "_" + sanitized
	}
	if len(sanitized) > 64 {
		sanitized = sanitized[:64]
	}
	return strings.TrimRight(sanitized, "")
}
''',
        encoding="utf-8",
        newline="\n",
    )

    (DST / "registry").mkdir(parents=True, exist_ok=True)
    (DST / "registry" / "model_info.go").write_text(
        '''package registry

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
''',
        encoding="utf-8",
        newline="\n",
    )

    init_path = DST / "translator" / "init.go"
    init_path.write_text(
        '''package translator

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
''',
        encoding="utf-8",
        newline="\n",
    )

    print("vendored cliproxy translator into", DST)


if __name__ == "__main__":
    main()
