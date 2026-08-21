package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net/http"

	cliproxytrans "gpt-load/internal/cliproxy/translator/translator"
	"gpt-load/internal/translator"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func (ps *ProxyServer) handleStreamingResponse(c *gin.Context, resp *http.Response) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		logrus.Error("Streaming unsupported by the writer, falling back to normal response")
		ps.handleNormalResponse(c, resp)
		return
	}

	buf := make([]byte, 4*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				logUpstreamError("writing stream to client", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			logUpstreamError("reading from upstream", err)
			return
		}
	}
}

func (ps *ProxyServer) handleNormalResponse(c *gin.Context, resp *http.Response) {
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		logUpstreamError("copying response body", err)
	}
}

func (ps *ProxyServer) writeConvertedResponse(c *gin.Context, resp *http.Response, route *protocolRoute, isStream bool) error {
	if isStream && translator.IsChatFormat(route.clientFormat) && translator.IsChatFormat(route.upstreamFormat) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Status(resp.StatusCode)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 52_428_800)
		var param any
		writer := &flushWriter{ResponseWriter: c.Writer}
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytesTrimSpace(line)) == 0 {
				continue
			}
			payload := append(append([]byte{}, line...), '\n')
			chunks := cliproxytrans.Response(
				string(route.upstreamFormat),
				string(route.clientFormat),
				c.Request.Context(),
				route.model,
				route.originalBody,
				route.translatedBody,
				payload,
				&param,
			)
			for _, chunk := range chunks {
				if err := writeResponsesSSEChunk(writer, chunk); err != nil {
					return err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		for _, chunk := range translator.FlushConvertedStream(
			c.Request.Context(),
			route.clientFormat,
			route.upstreamFormat,
			route.model,
			route.originalBody,
			route.translatedBody,
			&param,
		) {
			if err := writeResponsesSSEChunk(writer, chunk); err != nil {
				return err
			}
		}
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	body = handleGzipCompression(resp, body)
	converted, err := translator.ConvertResponseWithMeta(
		c.Request.Context(),
		route.clientFormat,
		route.upstreamFormat,
		route.model,
		route.originalBody,
		route.translatedBody,
		body,
		isStream,
	)
	if err != nil {
		return err
	}
	if isStream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
	} else {
		c.Header("Content-Type", "application/json")
	}
	c.Status(resp.StatusCode)
	_, err = c.Writer.Write(converted)
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return err
}

type flushWriter struct {
	gin.ResponseWriter
}

func (w *flushWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.Flush()
	return n, err
}

func writeResponsesSSEChunk(w io.Writer, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	if _, err := w.Write(chunk); err != nil {
		return err
	}
	if bytes.HasSuffix(chunk, []byte("\n\n")) || bytes.HasSuffix(chunk, []byte("\r\n\r\n")) {
		return nil
	}
	suffix := []byte("\n\n")
	if bytes.HasSuffix(chunk, []byte("\r\n")) {
		suffix = []byte("\r\n")
	} else if bytes.HasSuffix(chunk, []byte("\n")) {
		suffix = []byte("\n")
	}
	_, err := w.Write(suffix)
	return err
}

func bytesTrimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}
