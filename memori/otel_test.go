package memori

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOTelConfigDefaultsOff(t *testing.T) {
	t.Setenv("MEMORI_OTEL_ENDPOINT", "")
	t.Setenv("MEMORI_OTEL_SERVICE_NAME", "")
	c := newConfig()
	if c.Otel.Endpoint != "" || c.Otel.Service != "memorigo" {
		t.Fatalf("otel config = %#v", c.Otel)
	}
}

func TestOTelConfigFromEnv(t *testing.T) {
	t.Setenv("MEMORI_OTEL_ENDPOINT", "http://127.0.0.1:4318")
	t.Setenv("MEMORI_OTEL_SERVICE_NAME", "memorigo-test")
	c := newConfig()
	if c.Otel.Endpoint != "http://127.0.0.1:4318" || c.Otel.Service != "memorigo-test" {
		t.Fatalf("otel config = %#v", c.Otel)
	}
}

func TestOTelTracesURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{in: "", ok: false},
		{in: "http://127.0.0.1:4318", want: "http://127.0.0.1:4318/v1/traces", ok: true},
		{in: "http://127.0.0.1:4318/", want: "http://127.0.0.1:4318/v1/traces", ok: true},
		{in: "http://127.0.0.1:4318/v1/traces", want: "http://127.0.0.1:4318/v1/traces", ok: true},
		{in: "http://127.0.0.1:4318/otlp", want: "http://127.0.0.1:4318/otlp/v1/traces", ok: true},
		{in: "https://collector.example/v1/traces/", want: "https://collector.example/v1/traces", ok: true},
		{in: "ftp://collector.example", ok: false},
		{in: "http://user:pass@127.0.0.1/v1/traces", ok: false},
		{in: "http://127.0.0.1:4318/v1/traces?token=secret", ok: false},
		{in: "/v1/traces", ok: false},
	}
	for _, tc := range cases {
		got, ok := otelTracesURL(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("otelTracesURL(%q) = %q %v, want %q %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFinishSpanSkipsWhenUnsetOrInvalid(t *testing.T) {
	t.Setenv("MEMORI_OTEL_ENDPOINT", "")
	var mu sync.Mutex
	called := 0
	restore := swapOTelPost(func(string, []byte) {
		mu.Lock()
		called++
		mu.Unlock()
	})
	defer restore()

	m := &Memori{Config: newConfig()}
	m.finishSpan(otelSpanRecall, time.Now(), nil, otelFields{})
	m.Config.Otel.Endpoint = "ftp://collector"
	m.finishSpan(otelSpanRecall, time.Now(), nil, otelFields{})
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if called != 0 {
		t.Fatalf("posts = %d", called)
	}
}

func TestFinishSpanPostsCountsAndOmitsOperationText(t *testing.T) {
	secret := "otel-secret-phrase-9f3a"
	got := make(chan string, 1)
	restore := swapOTelPost(func(url string, body []byte) {
		if url != "http://127.0.0.1:4318/v1/traces" {
			t.Errorf("url = %s", url)
		}
		got <- string(body)
	})
	defer restore()

	m := &Memori{Config: &Config{Otel: OtelConfig{Endpoint: "http://127.0.0.1:4318"}}}
	start := time.Now().Add(-time.Millisecond)
	m.finishSpan(otelSpanRecall, start, errors.New(secret), otelFields{
		candidates: 4,
		hits:       1,
		tokens:     12,
		model:      "feature-hash-v1",
		dimension:  256,
	})
	select {
	case body := <-got:
		if strings.Contains(body, secret) {
			t.Fatalf("span contains operation text: %s", body)
		}
		span := decodeSpan(t, body)
		if span.Name != otelSpanRecall || span.Kind != 1 || span.Status.Code != 2 {
			t.Fatalf("span = %+v", span)
		}
		if len(span.TraceID) != 32 || len(span.SpanID) != 16 {
			t.Fatalf("ids = %s %s", span.TraceID, span.SpanID)
		}
		if span.attrInt(t, "memori.candidates") != 4 || span.attrInt(t, "memori.hits") != 1 || span.attrInt(t, "memori.tokens") != 12 {
			t.Fatalf("counts missing in %s", body)
		}
		if span.attrInt(t, "memori.embedding.dimension") != 256 || span.attrString("memori.embedding.model") != "feature-hash-v1" {
			t.Fatalf("embedding attrs missing in %s", body)
		}
		if span.service() != "memorigo" {
			t.Fatalf("service = %q", span.service())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("span was not posted")
	}
}

func swapOTelPost(fn func(string, []byte)) func() {
	prev := otelPost
	otelPost = fn
	return func() { otelPost = prev }
}

type postedSpan struct {
	Name       string
	Kind       int
	TraceID    string
	SpanID     string
	Attributes []struct {
		Key   string
		Value struct {
			IntValue    string `json:"intValue"`
			StringValue string `json:"stringValue"`
		}
	}
	Status struct {
		Code int
	}
	serviceName string
}

func decodeSpan(t *testing.T, body string) postedSpan {
	t.Helper()
	var doc struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []struct {
					Key   string
					Value struct {
						StringValue string `json:"stringValue"`
					}
				}
			}
			ScopeSpans []struct {
				Spans []postedSpan
			}
		}
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("unmarshal span: %v body=%s", err, body)
	}
	if len(doc.ResourceSpans) != 1 || len(doc.ResourceSpans[0].ScopeSpans) != 1 || len(doc.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatalf("span shape: %s", body)
	}
	span := doc.ResourceSpans[0].ScopeSpans[0].Spans[0]
	for _, attr := range doc.ResourceSpans[0].Resource.Attributes {
		if attr.Key == "service.name" {
			span.serviceName = attr.Value.StringValue
		}
	}
	return span
}

func (s postedSpan) service() string { return s.serviceName }

func (s postedSpan) attrInt(t *testing.T, key string) int {
	t.Helper()
	for _, attr := range s.Attributes {
		if attr.Key != key {
			continue
		}
		n, err := json.Number(attr.Value.IntValue).Int64()
		if err != nil {
			t.Fatalf("%s intValue %q: %v", key, attr.Value.IntValue, err)
		}
		return int(n)
	}
	t.Fatalf("missing %s", key)
	return 0
}

func (s postedSpan) attrString(key string) string {
	for _, attr := range s.Attributes {
		if attr.Key == key {
			return attr.Value.StringValue
		}
	}
	return ""
}
