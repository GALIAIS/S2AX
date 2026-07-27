package invocationarchive

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type boundedCapture struct {
	mu        sync.Mutex
	limit     int
	buffer    bytes.Buffer
	total     int64
	truncated bool
}

func newBoundedCapture(limit int) *boundedCapture { return &boundedCapture{limit: limit} }

func (c *boundedCapture) Write(payload []byte) (int, error) {
	if c == nil || len(payload) == 0 {
		return len(payload), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total += int64(len(payload))
	remaining := c.limit - c.buffer.Len()
	if remaining <= 0 {
		c.truncated = true
		return len(payload), nil
	}
	if remaining < len(payload) {
		_, _ = c.buffer.Write(payload[:remaining])
		c.truncated = true
		return len(payload), nil
	}
	_, _ = c.buffer.Write(payload)
	return len(payload), nil
}

func (c *boundedCapture) snapshot() ([]byte, int64, bool) {
	if c == nil {
		return nil, 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...), c.total, c.truncated
}

type observingReadCloser struct {
	io.ReadCloser
	capture *boundedCapture
}

func (r *observingReadCloser) Read(payload []byte) (int, error) {
	n, err := r.ReadCloser.Read(payload)
	if n > 0 {
		_, _ = r.capture.Write(payload[:n])
	}
	return n, err
}

type captureResponseWriter struct {
	gin.ResponseWriter
	capture *boundedCapture
}

func (w *captureResponseWriter) Write(payload []byte) (int, error) {
	n, err := w.ResponseWriter.Write(payload)
	if n > 0 {
		_, _ = w.capture.Write(payload[:n])
	}
	return n, err
}

func (w *captureResponseWriter) WriteString(value string) (int, error) {
	n, err := w.ResponseWriter.WriteString(value)
	if n > 0 {
		_, _ = w.capture.Write([]byte(value[:n]))
	}
	return n, err
}

// ReadFrom must observe io.Copy paths, which may otherwise bypass Write.
func (w *captureResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			written, writeErr := w.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

type payloadEnvelope struct {
	Encoding string `json:"encoding"`
	Data     string `json:"data"`
}

func protectPayload(encryptor service.SecretEncryptor, payload []byte) (string, string, error) {
	if encryptor == nil {
		return "", "", ErrPayloadUnavailable
	}
	envelope := payloadEnvelope{Encoding: "base64", Data: base64.StdEncoding.EncodeToString(payload)}
	if utf8.Valid(payload) {
		envelope.Encoding = "utf8"
		envelope.Data = string(payload)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", "", err
	}
	ciphertext, err := encryptor.Encrypt(string(raw))
	if err != nil || strings.TrimSpace(ciphertext) == "" {
		if err == nil {
			err = ErrPayloadUnavailable
		}
		return "", "", err
	}
	return ciphertext, envelope.Encoding, nil
}

func revealPayload(encryptor service.SecretEncryptor, ciphertext, contentType, status string, total, captured int64, truncated bool) (PayloadView, error) {
	view := PayloadView{
		Status: status, ContentType: contentType, TotalBytes: total, CapturedBytes: captured, Truncated: truncated,
	}
	if strings.TrimSpace(ciphertext) == "" || status != "captured" {
		return view, nil
	}
	if encryptor == nil {
		return view, ErrPayloadUnavailable
	}
	plain, err := encryptor.Decrypt(ciphertext)
	if err != nil {
		return view, err
	}
	var envelope payloadEnvelope
	if err := json.Unmarshal([]byte(plain), &envelope); err != nil {
		return view, err
	}
	if envelope.Encoding != "utf8" && envelope.Encoding != "base64" {
		return view, ErrPayloadUnavailable
	}
	if envelope.Encoding == "base64" {
		if _, err := base64.StdEncoding.DecodeString(envelope.Data); err != nil {
			return view, err
		}
	}
	view.Available = true
	view.Encoding = envelope.Encoding
	view.Data = envelope.Data
	return view, nil
}

func (s *Service) GatewayMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || isWebSocketUpgrade(c.Request) {
			c.Next()
			return
		}
		cfg := s.activeConfig()
		mode := cfg.Resolve(apiKey.UserID, apiKey.GroupID, apiKey.ID)
		if mode == ModeOff {
			c.Next()
			return
		}

		requestCapture := newBoundedCapture(cfg.MaxRequestBytes)
		if c.Request != nil && c.Request.Body != nil {
			c.Request.Body = &observingReadCloser{ReadCloser: c.Request.Body, capture: requestCapture}
		}
		responseCapture := newBoundedCapture(cfg.MaxResponseBytes)
		c.Writer = &captureResponseWriter{ResponseWriter: c.Writer, capture: responseCapture}
		c.Next()
		s.enqueueHTTP(c, apiKey, cfg, mode, requestCapture, responseCapture)
	}
}

func (s *Service) enqueueHTTP(c *gin.Context, apiKey *service.APIKey, cfg Config, mode Mode, requestCapture, responseCapture *boundedCapture) {
	if s == nil || c == nil || apiKey == nil || mode == ModeOff {
		return
	}
	requestBytes, requestTotal, requestTruncated := requestCapture.snapshot()
	responseBytes, responseTotal, responseTruncated := responseCapture.snapshot()
	requestStatus := payloadStatus(requestBytes, requestTotal, requestTruncated)
	responseStatus := payloadStatus(responseBytes, responseTotal, responseTruncated)
	if mode == ModeRequestOnly {
		responseBytes, responseTotal, responseTruncated, responseStatus = nil, 0, false, "omitted"
	}
	candidate := s.newCandidate(c, apiKey, cfg, mode, "http", 0)
	candidate.request = capturedPayload{
		bytes: requestBytes, contentType: mediaType(c.GetHeader("Content-Type")), total: requestTotal,
		truncated: requestTruncated, status: requestStatus,
	}
	candidate.response = capturedPayload{
		bytes: responseBytes, contentType: mediaType(c.Writer.Header().Get("Content-Type")), total: responseTotal,
		truncated: responseTruncated, status: responseStatus,
	}
	candidate.httpStatus = c.Writer.Status()
	candidate.outcome = outcomeForHTTPStatus(candidate.httpStatus)
	s.enqueue(candidate)
}

func (s *Service) newCandidate(c *gin.Context, apiKey *service.APIKey, cfg Config, mode Mode, transport string, turn int) archiveCandidate {
	createdAt := time.Now().UTC()
	path := ""
	method := ""
	requestID := ""
	clientRequestID := ""
	clientIP := ""
	userAgent := ""
	if c != nil && c.Request != nil {
		path = c.Request.URL.Path
		method = c.Request.Method
		requestID, _ = c.Request.Context().Value(ctxkey.RequestID).(string)
		clientRequestID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		clientIP = middleware.SecurityClientIP(c)
		userAgent = c.Request.UserAgent()
	}
	return archiveCandidate{
		createdAt: createdAt, completedAt: time.Now().UTC(), expiresAt: createdAt.Add(time.Duration(cfg.RetentionDays) * 24 * time.Hour),
		configVersion: cfg.ConfigVersion, mode: mode, transport: transport, websocketTurn: turn,
		identity: archiveIdentity(apiKey), requestID: trimText(requestID, 128), clientRequestID: trimText(clientRequestID, 128),
		method: trimText(method, 16), path: trimText(path, 512), clientIP: trimText(clientIP, 64), userAgent: trimText(userAgent, 512),
	}
}

func payloadStatus(bytes []byte, total int64, truncated bool) string {
	if total == 0 {
		return "empty"
	}
	if len(bytes) == 0 {
		return "not_read"
	}
	if truncated {
		return "captured"
	}
	return "captured"
}

func isWebSocketUpgrade(request *http.Request) bool {
	return request != nil && strings.EqualFold(strings.TrimSpace(request.Header.Get("Upgrade")), "websocket")
}

func mediaType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(value)
	if err == nil {
		return trimText(strings.ToLower(parsed), 255)
	}
	return trimText(strings.ToLower(value), 255)
}

func extractModel(payload []byte, path string) string {
	if model := strings.TrimSpace(gjson.GetBytes(payload, "model").String()); model != "" {
		return trimText(model, 255)
	}
	const marker = "/models/"
	if index := strings.Index(path, marker); index >= 0 {
		model := path[index+len(marker):]
		if colon := strings.IndexByte(model, ':'); colon >= 0 {
			model = model[:colon]
		}
		return trimText(model, 255)
	}
	return ""
}

func outcomeForHTTPStatus(status int) string {
	switch {
	case status >= 500:
		return "server_error"
	case status >= 400:
		return "client_error"
	default:
		return "completed"
	}
}

func trimText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type websocketPending struct {
	candidate       archiveCandidate
	responseCapture *boundedCapture
}

// WebSocketSession keeps a bounded client-visible transcript per Responses WS
// turn. It is only created for an enabled archive policy.
type WebSocketSession struct {
	service *Service
	config  Config
	mode    Mode
	apiKey  *service.APIKey
	context *gin.Context

	mu    sync.Mutex
	turns map[int]*websocketPending
}

func (s *Service) BeginWebSocketSession(c *gin.Context, apiKey *service.APIKey, initialPayload []byte, model string) *WebSocketSession {
	if s == nil || c == nil || apiKey == nil {
		return nil
	}
	cfg := s.activeConfig()
	mode := cfg.Resolve(apiKey.UserID, apiKey.GroupID, apiKey.ID)
	if mode == ModeOff {
		return nil
	}
	session := &WebSocketSession{
		service: s, config: cfg, mode: mode, apiKey: apiKey, context: c,
		turns: make(map[int]*websocketPending),
	}
	session.CaptureTurnRequest(1, initialPayload, model)
	return session
}

func (s *WebSocketSession) CaptureTurnRequest(turn int, payload []byte, model string) {
	if s == nil || turn < 1 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, exists := s.turns[turn]; exists {
		if current.candidate.model == "" {
			current.candidate.model = trimText(model, 255)
		}
		return
	}
	candidate := s.service.newCandidate(s.context, s.apiKey, s.config, s.mode, "websocket", turn)
	candidate.model = trimText(model, 255)
	candidate.request = captureWebSocketPayload(payload, s.config.MaxRequestBytes)
	if s.mode == ModeRequestOnly {
		candidate.response = capturedPayload{status: "omitted"}
	}
	s.turns[turn] = &websocketPending{candidate: candidate, responseCapture: newBoundedCapture(s.config.MaxResponseBytes)}
}

func (s *WebSocketSession) CaptureClientMessage(turn int, payload []byte) {
	if s == nil || turn < 1 || len(payload) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pending := s.turns[turn]; pending != nil && s.mode == ModeFull {
		_, _ = pending.responseCapture.Write(payload)
	}
}

func (s *WebSocketSession) FinishTurn(turn int, result *service.OpenAIForwardResult, turnErr error) {
	if s == nil || turn < 1 {
		return
	}
	s.mu.Lock()
	pending := s.turns[turn]
	delete(s.turns, turn)
	s.mu.Unlock()
	if pending == nil {
		return
	}
	candidate := pending.candidate
	candidate.completedAt = time.Now().UTC()
	candidate.httpStatus = http.StatusSwitchingProtocols
	if result != nil && result.Model != "" {
		candidate.model = trimText(result.Model, 255)
	}
	if turnErr != nil {
		candidate.outcome = "websocket_error"
	} else {
		candidate.outcome = "completed"
	}
	if s.mode == ModeFull {
		payload, total, truncated := pending.responseCapture.snapshot()
		candidate.response = capturedPayload{
			bytes: payload, contentType: "application/json", total: total, truncated: truncated,
			status: payloadStatus(payload, total, truncated),
		}
	}
	s.service.enqueue(candidate)
}

func captureWebSocketPayload(payload []byte, limit int) capturedPayload {
	capture := newBoundedCapture(limit)
	_, _ = capture.Write(payload)
	bytes, total, truncated := capture.snapshot()
	return capturedPayload{
		bytes: bytes, contentType: "application/json", total: total, truncated: truncated,
		status: payloadStatus(bytes, total, truncated),
	}
}
