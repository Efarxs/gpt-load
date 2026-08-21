package proxy

import (
	"net/url"
	"strings"

	"gpt-load/internal/models"
	"gpt-load/internal/translator"
)

type protocolRoute struct {
	clientFormat   translator.Format
	upstreamFormat translator.Format
	converted      bool
	rewritePath    string
	stream         bool
	model          string
	originalBody   []byte
	translatedBody []byte
}

func applyProtocolRouting(group *models.Group, groupName, requestPath, contentType string, body []byte) ([]byte, *protocolRoute, error) {
	route := &protocolRoute{}
	if group == nil || !group.EffectiveConfig.EnableProtocolRouting {
		return body, route, nil
	}

	clientPath := strings.TrimPrefix(requestPath, "/proxy/"+groupName)
	source := translator.DetectFromPath(clientPath)
	if source == translator.FormatUnknown {
		return body, route, nil
	}
	target := translator.FormatFromChannel(group.ChannelType)
	route.clientFormat = source
	route.upstreamFormat = target

	workBody := body
	if source == translator.FormatImages && strings.Contains(strings.ToLower(contentType), "multipart/") {
		if translator.CompatibleIdentity(source, target) {
			return body, route, nil
		}
		converted, err := translator.ImagesJSONFromMultipart(contentType, body)
		if err != nil {
			return nil, nil, err
		}
		workBody = converted
	}

	out, err := translator.ConvertRequest(source, target, clientPath, workBody)
	if err != nil {
		return nil, nil, err
	}
	route.converted = out.Converted
	route.stream = out.Stream || translator.DetectStream(workBody)
	route.model = out.Model
	route.originalBody = out.Original
	route.translatedBody = out.Body
	if out.Path != "" {
		route.rewritePath = "/proxy/" + groupName + out.Path
	}
	return out.Body, route, nil
}

func rewriteRequestURL(original *url.URL, rewritePath string) *url.URL {
	if original == nil || rewritePath == "" {
		return original
	}
	cloned := *original
	cloned.Path = rewritePath
	cloned.RawPath = ""
	return &cloned
}
