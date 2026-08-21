package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/models"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (ps *ProxyServer) applyParamOverrides(bodyBytes []byte, group *models.Group) ([]byte, error) {
	if len(group.ParamOverrides) == 0 || len(bodyBytes) == 0 {
		return bodyBytes, nil
	}

	var requestData map[string]any
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		logrus.Warnf("failed to unmarshal request body for param override, passing through: %v", err)
		return bodyBytes, nil
	}

	for key, value := range group.ParamOverrides {
		requestData[key] = value
	}

	return json.Marshal(requestData)
}

func applyOutboundBodyFixes(body []byte, group *models.Group) []byte {
	if len(body) == 0 || !gjson.ValidBytes(body) || group == nil {
		return body
	}

	out := body
	model := strings.TrimSpace(gjson.GetBytes(out, "model").String())
	if overrides := lookupModelParamOverrides(group, model); len(overrides) > 0 {
		for key, value := range overrides {
			if strings.TrimSpace(key) == "" {
				continue
			}
			patched, err := sjson.SetBytes(out, key, value)
			if err != nil {
				logrus.WithError(err).WithField("key", key).Warn("failed to apply model param override")
				continue
			}
			out = patched
		}
	}

	if shouldStripOrphanToolFlags(group) {
		out = stripOrphanToolFlags(out)
	}
	return out
}

func shouldStripOrphanToolFlags(group *models.Group) bool {
	if group == nil {
		return true
	}
	switch group.ChannelType {
	case "anthropic", "gemini":
		return false
	}
	return group.EffectiveConfig.StripOrphanToolFlags
}

func stripOrphanToolFlags(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		return body
	}
	out := body
	if gjson.GetBytes(out, "tool_choice").Exists() {
		out, _ = sjson.DeleteBytes(out, "tool_choice")
	}
	if gjson.GetBytes(out, "parallel_tool_calls").Exists() {
		out, _ = sjson.DeleteBytes(out, "parallel_tool_calls")
	}
	return out
}

func lookupModelParamOverrides(group *models.Group, model string) map[string]any {
	if group == nil || len(group.ModelParamOverrides) == 0 {
		return nil
	}
	merged := map[string]any{}
	appendOverride := func(key string) {
		raw, ok := group.ModelParamOverrides[key]
		if !ok || raw == nil {
			return
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			return
		}
		for k, v := range obj {
			merged[k] = v
		}
	}
	appendOverride("*")
	if model != "" {
		appendOverride(model)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// logUpstreamError provides a centralized way to log errors from upstream interactions.
func logUpstreamError(context string, err error) {
	if err == nil {
		return
	}
	if app_errors.IsIgnorableError(err) {
		logrus.Debugf("Ignorable upstream error in %s: %v", context, err)
	} else {
		logrus.Errorf("Upstream error in %s: %v", context, err)
	}
}

// handleGzipCompression checks for gzip encoding and decompresses the body if necessary.
func handleGzipCompression(resp *http.Response, bodyBytes []byte) []byte {
	if resp.Header.Get("Content-Encoding") == "gzip" {
		reader, gzipErr := gzip.NewReader(bytes.NewReader(bodyBytes))
		if gzipErr != nil {
			logrus.Warnf("Failed to create gzip reader for error body: %v", gzipErr)
			return bodyBytes
		}
		defer reader.Close()

		decompressedBody, readAllErr := io.ReadAll(reader)
		if readAllErr != nil {
			logrus.Warnf("Failed to decompress gzip error body: %v", readAllErr)
			return bodyBytes
		}
		return decompressedBody
	}
	return bodyBytes
}
