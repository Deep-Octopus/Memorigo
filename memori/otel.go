package memori

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	otelSpanRecall       = "memori.recall"
	otelSpanRemember     = "memori.remember"
	otelSpanBuildContext = "memori.build_context"
	otelExportTimeout    = 200 * time.Millisecond
)

// otelFields are counts and embedding identity. They never carry the query
// or the memory text. JSONL audit stays the audit record.
type otelFields struct {
	candidates int
	hits       int
	tokens     int
	skipped    string
	model      string
	dimension  int
}

type recallMeta struct {
	candidates int
	skipped    string
}

var (
	otelEndpointWarn sync.Once
	otelHTTP         = &http.Client{
		Timeout: otelExportTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// otelPost sends one OTLP/HTTP JSON body. A failure is ignored by the caller.
	otelPost = postOTLP
)

func embedModel(m *Memori) string {
	if m == nil || m.Embedder == nil {
		return ""
	}
	return m.Embedder.Model()
}

func embedDim(m *Memori) int {
	if m == nil || m.Embedder == nil {
		return 0
	}
	return m.Embedder.Dimension()
}

// finishSpan exports one span when an endpoint is configured. It returns
// before the POST finishes. A collector error, a bad URL, or a full timeout
// does not change the operation result.
func (m *Memori) finishSpan(name string, start time.Time, opErr error, fields otelFields) {
	if m == nil || m.Config == nil || name == "" {
		return
	}
	m.Config.mu.RLock()
	endpoint := m.Config.Otel.Endpoint
	service := m.Config.Otel.Service
	m.Config.mu.RUnlock()
	if strings.TrimSpace(endpoint) == "" {
		return
	}
	tracesURL, ok := otelTracesURL(endpoint)
	if !ok {
		if m.logger != nil {
			otelEndpointWarn.Do(func() {
				m.logger.Warn("MEMORI_OTEL_ENDPOINT is not an http(s) URL; spans are not exported")
			})
		}
		return
	}
	if strings.TrimSpace(service) == "" {
		service = "memorigo"
	}
	body, ok := marshalOTLPSpan(name, service, start, time.Now(), opErr, fields)
	if !ok {
		return
	}
	go otelPost(tracesURL, body)
}

func postOTLP(tracesURL string, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), otelExportTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tracesURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := otelHTTP.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
}

// otelTracesURL accepts an absolute http(s) base URL and returns the OTLP
// traces path. Userinfo, query, and fragment are rejected so a credential
// cannot ride along in the endpoint. A path that already ends in /v1/traces
// is kept.
func otelTracesURL(endpoint string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/v1/traces") {
		if path == "" {
			path = "/v1/traces"
		} else {
			path += "/v1/traces"
		}
	}
	u.Path = path
	u.RawPath = ""
	return u.String(), true
}

func marshalOTLPSpan(name, service string, start, end time.Time, opErr error, fields otelFields) ([]byte, bool) {
	traceID, spanID, ok := newSpanIDs()
	if !ok {
		return nil, false
	}
	if end.Before(start) {
		end = start
	}
	startNano := start.UnixNano()
	endNano := end.UnixNano()
	if startNano < 0 || endNano < 0 {
		return nil, false
	}
	status := 1 // STATUS_CODE_OK
	if opErr != nil {
		status = 2 // STATUS_CODE_ERROR
	}
	attrs := []otlpAttr{
		intAttr("memori.candidates", fields.candidates),
		intAttr("memori.hits", fields.hits),
		intAttr("memori.tokens", fields.tokens),
		strAttr("memori.embedding.model", fields.model),
		intAttr("memori.embedding.dimension", fields.dimension),
	}
	if fields.skipped != "" {
		attrs = append(attrs, strAttr("memori.skipped", fields.skipped))
	}
	doc := otlpExport{ResourceSpans: []otlpResourceSpans{{
		Resource: otlpResource{Attributes: []otlpAttr{strAttr("service.name", service)}},
		ScopeSpans: []otlpScopeSpans{{
			Scope: otlpScope{Name: "github.com/Deep-Octopus/Memorigo/memori"},
			Spans: []otlpSpan{{
				TraceID:           traceID,
				SpanID:            spanID,
				Name:              name,
				Kind:              1, // SPAN_KIND_INTERNAL
				StartTimeUnixNano: strconv.FormatInt(startNano, 10),
				EndTimeUnixNano:   strconv.FormatInt(endNano, 10),
				Attributes:        attrs,
				Status:            otlpStatus{Code: status},
			}},
		}},
	}}}
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, false
	}
	return body, true
}

func newSpanIDs() (traceID, spanID string, ok bool) {
	var trace [16]byte
	var span [8]byte
	if _, err := rand.Read(trace[:]); err != nil {
		return "", "", false
	}
	if _, err := rand.Read(span[:]); err != nil {
		return "", "", false
	}
	if allZero(trace[:]) {
		trace[15] = 1
	}
	if allZero(span[:]) {
		span[7] = 1
	}
	return hex.EncodeToString(trace[:]), hex.EncodeToString(span[:]), true
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func strAttr(key, value string) otlpAttr {
	return otlpAttr{Key: key, Value: otlpValue{StringValue: value}}
}

func intAttr(key string, n int) otlpAttr {
	return otlpAttr{Key: key, Value: otlpValue{IntValue: strconv.Itoa(n)}}
}

// OTLP/HTTP JSON uses the proto3 JSON mapping: int64 and fixed64 are strings.
type otlpExport struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttr `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []otlpAttr `json:"attributes,omitempty"`
	Status            otlpStatus `json:"status"`
}

type otlpStatus struct {
	Code int `json:"code"`
}

type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue string `json:"stringValue,omitempty"`
	IntValue    string `json:"intValue,omitempty"`
}
