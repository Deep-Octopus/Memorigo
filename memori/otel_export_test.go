package memori_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Deep-Octopus/Memorigo/memori"
)

func TestOTelExportOmitsMemoryText(t *testing.T) {
	sink := newSpanSink(t, http.StatusOK)
	const secret = "otel-secret-phrase-9f3a"
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_OTEL_ENDPOINT":     sink.URL,
		"MEMORI_OTEL_SERVICE_NAME": "memorigo-otel-test",
	})
	if err := m.TryAttribution("user", "proc"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I like " + secret); err != nil {
		t.Fatal(err)
	}
	facts, err := m.Recall(secret, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) == 0 {
		t.Fatal("recall returned no facts")
	}
	pkg, err := m.BuildContext(context.Background(), secret, memori.ContextOptions{TokenBudget: 200})
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Tokens <= 0 {
		t.Fatalf("tokens = %d", pkg.Tokens)
	}
	if _, err := m.Recall("hello", 5); err != nil {
		t.Fatal(err)
	}

	bodies := sink.wait(t, 4)
	names := map[string]bool{}
	for _, body := range bodies {
		if strings.Contains(body, secret) || strings.Contains(body, "hello") {
			t.Fatalf("span contains query or memory text: %s", body)
		}
		span := decodeExportedSpan(t, body)
		names[span.Name] = true
		if span.service() != "memorigo-otel-test" {
			t.Fatalf("service = %q", span.service())
		}
		if span.Status.Code != 1 {
			t.Fatalf("status = %d", span.Status.Code)
		}
	}
	for _, name := range []string{"memori.remember", "memori.recall", "memori.build_context"} {
		if !names[name] {
			t.Fatalf("missing %s in %v", name, names)
		}
	}
}

func TestOTelExportFailureDoesNotFailTheCall(t *testing.T) {
	sink := newSpanSink(t, http.StatusInternalServerError)
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_OTEL_ENDPOINT": sink.URL,
	})
	if err := m.TryAttribution("user", "proc"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Remember("I like tea"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recall("tea", 5); err != nil {
		t.Fatal(err)
	}
	sink.wait(t, 2)

	down := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_OTEL_ENDPOINT": "http://127.0.0.1:1",
	})
	if err := down.TryAttribution("user", "proc"); err != nil {
		t.Fatal(err)
	}
	if _, err := down.Recall("tea", 3); err != nil {
		t.Fatal(err)
	}
}

func TestOTelRememberErrorIsStillReturned(t *testing.T) {
	sink := newSpanSink(t, http.StatusOK)
	m := newTestMemoriWithEnv(t, map[string]string{
		"MEMORI_OTEL_ENDPOINT": sink.URL,
	})
	_, err := m.Remember("I like otel-unattributed-tea")
	if !errors.Is(err, memori.ErrNoAttribution) {
		t.Fatalf("err = %v", err)
	}
	body := sink.wait(t, 1)[0]
	if strings.Contains(body, "otel-unattributed-tea") {
		t.Fatalf("span contains memory text: %s", body)
	}
	span := decodeExportedSpan(t, body)
	if span.Name != "memori.remember" || span.Status.Code != 2 {
		t.Fatalf("span = %+v", span)
	}
}

func TestOTelDefaultDoesNotExport(t *testing.T) {
	sink := newSpanSink(t, http.StatusOK)
	m := newTestMemori(t)
	if err := m.TryAttribution("user", "proc"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recall("hello", 3); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := sink.count(); n != 0 {
		t.Fatalf("posts = %d to %s", n, sink.URL)
	}
}

type spanSink struct {
	URL string
	mu  sync.Mutex
	got []string
}

func newSpanSink(t *testing.T, status int) *spanSink {
	t.Helper()
	sink := &spanSink{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		sink.mu.Lock()
		sink.got = append(sink.got, string(body))
		sink.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	sink.URL = srv.URL
	return sink
}

func (s *spanSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func (s *spanSink) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.got) >= n {
			out := append([]string(nil), s.got...)
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("got %d spans, want %d", s.count(), n)
	return nil
}

type exportedSpan struct {
	Name   string
	Status struct {
		Code int
	}
	serviceName string
}

func decodeExportedSpan(t *testing.T, body string) exportedSpan {
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
				Spans []exportedSpan
			}
		}
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.ResourceSpans) != 1 || len(doc.ResourceSpans[0].ScopeSpans) != 1 || len(doc.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatalf("shape: %s", body)
	}
	span := doc.ResourceSpans[0].ScopeSpans[0].Spans[0]
	for _, attr := range doc.ResourceSpans[0].Resource.Attributes {
		if attr.Key == "service.name" {
			span.serviceName = attr.Value.StringValue
		}
	}
	return span
}

func (s exportedSpan) service() string { return s.serviceName }
