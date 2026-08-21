package translator

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

// ImagesJSONFromMultipart 把 Images edits 的 multipart 表单收成 JSON，便于再转 Responses。
func ImagesJSONFromMultipart(contentType string, body []byte) ([]byte, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return nil, &ConvertError{Status: http.StatusBadRequest, Message: "Images edits 需要 multipart/form-data 或 JSON"}
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, &ConvertError{Status: http.StatusBadRequest, Message: "解析 multipart 失败: " + err.Error()}
		}
		name := part.FormName()
		if name == "" {
			continue
		}
		if name == "image" || name == "mask" {
			continue
		}
		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return nil, &ConvertError{Status: http.StatusBadRequest, Message: "读取 multipart 字段失败: " + err.Error()}
		}
		fields[name] = string(value)
	}
	payload := map[string]any{}
	for k, v := range fields {
		payload[k] = v
	}
	return json.Marshal(payload)
}
