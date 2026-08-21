package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gpt-load/internal/models"
	"gpt-load/internal/utils"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	Register("openai-response", newOpenAIResponseChannel)
}

type OpenAIResponseChannel struct {
	*BaseChannel
}

func newOpenAIResponseChannel(f *Factory, group *models.Group) (ChannelProxy, error) {
	base, err := f.newBaseChannel("openai-response", group)
	if err != nil {
		return nil, err
	}

	return &OpenAIResponseChannel{
		BaseChannel: base,
	}, nil
}

func (ch *OpenAIResponseChannel) ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group) {
	req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
}

func (ch *OpenAIResponseChannel) IsStreamRequest(c *gin.Context, bodyBytes []byte) bool {
	if strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		return true
	}

	if c.Query("stream") == "true" {
		return true
	}

	type streamPayload struct {
		Stream bool `json:"stream"`
	}
	var p streamPayload
	if err := json.Unmarshal(bodyBytes, &p); err == nil {
		return p.Stream
	}

	return false
}

func (ch *OpenAIResponseChannel) ExtractModel(c *gin.Context, bodyBytes []byte) string {
	type modelPayload struct {
		Model string `json:"model"`
	}
	var p modelPayload
	if err := json.Unmarshal(bodyBytes, &p); err == nil {
		return p.Model
	}
	return ""
}

func (ch *OpenAIResponseChannel) ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) KeyProbeResult {
	upstreamURL := ch.getUpstreamURL()
	if upstreamURL == nil {
		return KeyProbeResult{Err: fmt.Errorf("no upstream URL configured for channel %s", ch.Name)}
	}

	endpointURL, err := url.Parse(ch.ValidationEndpoint)
	if err != nil {
		return KeyProbeResult{Err: fmt.Errorf("failed to parse validation endpoint: %w", err)}
	}

	finalURL := *upstreamURL
	finalURL.Path = strings.TrimRight(finalURL.Path, "/") + endpointURL.Path
	finalURL.RawQuery = endpointURL.RawQuery
	reqURL := finalURL.String()

	payload := gin.H{
		"model": ch.TestModel,
		"input": "hi",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return KeyProbeResult{Err: fmt.Errorf("failed to marshal validation payload: %w", err)}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewBuffer(body))
	if err != nil {
		return KeyProbeResult{Err: fmt.Errorf("failed to create validation request: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey.KeyValue)
	req.Header.Set("Content-Type", "application/json")

	if len(group.HeaderRuleList) > 0 {
		headerCtx := utils.NewHeaderVariableContext(group, apiKey)
		utils.ApplyHeaderRules(req, group.HeaderRuleList, headerCtx)
	}

	resp, err := ch.HTTPClient.Do(req)
	if err != nil {
		return KeyProbeResult{Err: fmt.Errorf("failed to send validation request: %w", err)}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return KeyProbeResult{Err: fmt.Errorf("failed to read validation response: %w", err)}
	}
	return newKeyProbeResult(resp.StatusCode, respBody, group, "openai-response")
}
