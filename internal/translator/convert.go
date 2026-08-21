package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	cliproxytrans "gpt-load/internal/cliproxy/translator/translator"

	_ "gpt-load/internal/cliproxy/translator" // 注册完整转换器
)

// ConvertError 表示无法转换或不支持的协议对。
type ConvertError struct {
	Status  int
	Message string
}

func (e *ConvertError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func unsupportedPair(source, target Format) *ConvertError {
	return &ConvertError{
		Status:  http.StatusBadRequest,
		Message: fmt.Sprintf("不支持将 %s 转换为 %s", source, target),
	}
}

// RequestOutcome 是请求转换结果。
type RequestOutcome struct {
	Body      []byte
	Original  []byte
	Path      string
	Source    Format
	Target    Format
	Converted bool
	Stream    bool
	Model     string
}

// ConvertRequest 把客户端请求转换成上游协议。
// 对话协议走 CLIProxyAPI 完整转换器，不在这里裁剪字段。
func ConvertRequest(source, target Format, originalPath string, body []byte) (*RequestOutcome, error) {
	out := &RequestOutcome{
		Body:     body,
		Original: body,
		Path:     originalPath,
		Source:   source,
		Target:   target,
	}
	if source == FormatUnknown {
		return out, nil
	}
	if !SupportsConversion(source, target) {
		return nil, unsupportedPair(source, target)
	}

	stream := detectStream(body)
	model := extractModel(body)
	out.Stream = stream
	out.Model = model
	out.Path = RewritePath(source, target, originalPath, model, stream)

	if CompatibleIdentity(source, target) {
		return out, nil
	}

	if source == FormatImages && target == FormatOpenAIResponse {
		converted, err := imagesToResponses(originalPath, body)
		if err != nil {
			return nil, err
		}
		out.Body = converted
		out.Converted = true
		out.Stream = false
		out.Path = "/v1/responses"
		if m := extractModel(converted); m != "" {
			out.Model = m
		}
		return out, nil
	}

	if model == "" {
		model = "gpt-4"
	}
	encoded := cliproxytrans.Request(string(source), string(target), model, body, stream)
	out.Body = encoded
	out.Converted = true
	if m := extractModel(encoded); m != "" {
		out.Model = m
	}
	out.Path = RewritePath(source, target, originalPath, out.Model, stream)
	return out, nil
}

// ConvertResponse 把上游响应转换成客户端协议。
func ConvertResponse(source, target Format, upstreamBody []byte, stream bool) ([]byte, error) {
	return ConvertResponseWithMeta(context.Background(), source, target, "", nil, nil, upstreamBody, stream)
}

// ConvertResponseWithMeta 使用 CLIProxyAPI 转换器，带上原始/已转换请求以便还原工具名等状态。
func ConvertResponseWithMeta(
	ctx context.Context,
	source, target Format,
	model string,
	originalRequest, translatedRequest, upstreamBody []byte,
	stream bool,
) ([]byte, error) {
	if !sourceNeedsResponseConvert(source, target) {
		return upstreamBody, nil
	}
	if source == FormatImages && target == FormatOpenAIResponse {
		return responsesToImages(upstreamBody)
	}
	if model == "" {
		model = extractModel(translatedRequest)
	}
	if stream && len(originalRequest) == 0 {
		originalRequest = []byte(`{"stream":true}`)
	}
	if stream {
		return convertCliproxyStream(ctx, source, target, model, originalRequest, translatedRequest, upstreamBody)
	}
	var param any
	return cliproxytrans.ResponseNonStream(
		string(target),
		string(source),
		ctx,
		model,
		originalRequest,
		translatedRequest,
		upstreamBody,
		&param,
	), nil
}

func convertCliproxyStream(
	ctx context.Context,
	source, target Format,
	model string,
	originalRequest, translatedRequest, upstreamBody []byte,
) ([]byte, error) {
	var out bytes.Buffer
	var param any
	for _, line := range bytes.Split(upstreamBody, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		chunks := cliproxytrans.Response(
			string(target),
			string(source),
			ctx,
			model,
			originalRequest,
			translatedRequest,
			append(append([]byte{}, line...), '\n'),
			&param,
		)
		for _, chunk := range chunks {
			writeConvertedSSE(&out, chunk)
		}
	}
	for _, chunk := range FlushConvertedStream(ctx, source, target, model, originalRequest, translatedRequest, &param) {
		writeConvertedSSE(&out, chunk)
	}
	return out.Bytes(), nil
}

// FlushConvertedStream 在上游流结束时补发终态（如 response.completed），避免缺少 [DONE] 时 Codex 报断流。
func FlushConvertedStream(
	ctx context.Context,
	source, target Format,
	model string,
	originalRequest, translatedRequest []byte,
	param *any,
) [][]byte {
	if param == nil || *param == nil {
		return nil
	}
	return cliproxytrans.Response(
		string(target),
		string(source),
		ctx,
		model,
		originalRequest,
		translatedRequest,
		[]byte("data: [DONE]\n"),
		param,
	)
}

func writeConvertedSSE(buf *bytes.Buffer, chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	buf.Write(chunk)
	if bytes.HasSuffix(chunk, []byte("\n\n")) || bytes.HasSuffix(chunk, []byte("\r\n\r\n")) {
		return
	}
	if bytes.HasSuffix(chunk, []byte("\n")) {
		buf.WriteByte('\n')
		return
	}
	buf.WriteString("\n\n")
}

func sourceNeedsResponseConvert(source, target Format) bool {
	if CompatibleIdentity(source, target) {
		return false
	}
	return SupportsConversion(source, target)
}

// DetectStream 判断请求是否要求流式。JSON 解析失败时回退扫描 stream 字段。
func DetectStream(body []byte) bool {
	return detectStream(body)
}

func detectStream(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var probe struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &probe); err == nil {
		return probe.Stream
	}
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, `"stream":true`) || strings.Contains(lower, `"stream": true`)
}

func extractModel(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.Model)
}
