package channel

import (
	"context"
	"gpt-load/internal/models"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// ChannelProxy defines the interface for different API channel proxies.
type ChannelProxy interface {
	// BuildUpstreamURL 按加权轮询构造上游 URL，并返回选中的上游下标。
	BuildUpstreamURL(originalURL *url.URL, groupName string) (string, int, error)

	// BuildUpstreamURLAt 按指定下标重放上游。
	BuildUpstreamURLAt(originalURL *url.URL, groupName string, idx int) (string, error)

	// UpstreamBaseURL 返回指定下标上游的基址，越界时为空。
	UpstreamBaseURL(idx int) string

	// IsConfigStale checks if the channel's configuration is stale compared to the provided group.
	IsConfigStale(group *models.Group) bool

	// GetHTTPClient returns the client for standard requests.
	GetHTTPClient() *http.Client

	// GetStreamClient returns the client for streaming requests.
	GetStreamClient() *http.Client

	// ModifyRequest allows the channel to add specific headers or modify the request
	ModifyRequest(req *http.Request, apiKey *models.APIKey, group *models.Group)

	// IsStreamRequest checks if the request is for a streaming response,
	IsStreamRequest(c *gin.Context, bodyBytes []byte) bool

	// ExtractModel extracts the model name from the request.
	ExtractModel(c *gin.Context, bodyBytes []byte) string

	// ValidateKey 探测密钥是否可用，并返回状态码与上游正文。
	ValidateKey(ctx context.Context, apiKey *models.APIKey, group *models.Group) KeyProbeResult

	// ApplyModelRedirect applies model redirection based on the group's redirect rules.
	ApplyModelRedirect(req *http.Request, bodyBytes []byte, group *models.Group) ([]byte, error)

	// TransformModelList transforms the model list response based on redirect rules.
	TransformModelList(req *http.Request, bodyBytes []byte, group *models.Group) (map[string]any, error)
}
