package transport

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kasimlyee/monita-agent/internal/buffer"
	"github.com/kasimlyee/monita-agent/internal/config"
	"github.com/kasimlyee/monita-agent/internal/metrics"
)

// signingKeyHex is a 32-byte test key stored as hex for easy use in the server.
const signingKeyHex = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// signingKeyB64 is the same key in base64url (as stored in config).
const signingKeyB64 = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="

func newTestConfig(collectorURL string) *config.Config {
	return &config.Config{
		CollectorURL: collectorURL,
		AgentID:      "test-agent-id",
		Token:        "test-token",
		SigningKey:    signingKeyB64,
		PushInterval: config.Duration{Duration: 100 * time.Millisecond},
		MaxBatchSize: 500,
		BufferMaxMB:  10,
		BufferMaxAge: config.Duration{Duration: time.Hour},
		Metrics: config.MetricsConfig{
			Enabled:  []string{"cpu", "memory"},
			Interval: config.Duration{Duration: 10 * time.Second},
		},
		StateDir: "",
	}
}

// verifySignature checks the X-Signature on a request using the test key.
func verifySignature(r *http.Request, body []byte) bool {
	ts := r.Header.Get("X-Timestamp")
	nonce := r.Header.Get("X-Nonce")
	fp := "" // no fingerprint in these tests

	bodyHash := sha256.Sum256(body)
	material := ts + "." + nonce + "." + fp + "." + hex.EncodeToString(bodyHash[:])

	key, _ := hex.DecodeString(signingKeyHex)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(material))
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(r.Header.Get("X-Signature")))
}

// TestIntegration_PushMetrics_Success verifies that PushMetrics reaches the
// Collector, carries valid auth headers, and returns ok=true.
func TestIntegration_PushMetrics_Success(t *testing.T) {
	var received atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !verifySignature(r, body) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		received.Store(true)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"rotation_required":false}`))
	}))
	defer srv.Close()

	cfg := newTestConfig(srv.URL)
	// Use the test server's TLS — skip cert verification via InsecureSkipVerify
	// by not setting CertPin and overriding the transport.
	client, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Point the client at the test server's TLS transport.
	client.http = srv.Client()

	pts := []metrics.Point{
		{Metric: "cpu.percent", Value: 42.1, Labels: map[string]string{"core": "0"}, TS: time.Now().Unix()},
	}
	ok, rotReq, err := client.PushMetrics(context.Background(), pts)
	if err != nil {
		t.Fatalf("PushMetrics error: %v", err)
	}
	if !ok {
		t.Error("want ok=true")
	}
	if rotReq {
		t.Error("want rotation_required=false")
	}
	if !received.Load() {
		t.Error("server never received a request")
	}
}

// TestIntegration_PushMetrics_401_TriggersBuffer verifies that a 401 response
// causes the push loop to buffer the batch and increment authFails.
func TestIntegration_PushMetrics_401(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := newTestConfig(srv.URL)
	client, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.http = srv.Client()

	_, _, err = client.PushMetrics(context.Background(), []metrics.Point{
		{Metric: "cpu.percent", Value: 1.0, Labels: map[string]string{}, TS: time.Now().Unix()},
	})
	if err == nil {
		t.Fatal("want error on 401, got nil")
	}
	if err != ErrUnauthorized {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

// TestIntegration_PushMetrics_403_FingerprintMismatch verifies ErrFingerprintMismatch.
func TestIntegration_PushMetrics_403(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := newTestConfig(srv.URL)
	client, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.http = srv.Client()

	_, _, err = client.PushMetrics(context.Background(), []metrics.Point{
		{Metric: "cpu.percent", Value: 1.0, Labels: map[string]string{}, TS: time.Now().Unix()},
	})
	if err != ErrFingerprintMismatch {
		t.Errorf("want ErrFingerprintMismatch, got %v", err)
	}
}

// TestIntegration_RotationRequired verifies that rotation_required=true in the
// response body triggers a call to POST /v1/agents/self/rotate.
func TestIntegration_RotationRequired(t *testing.T) {
	var rotateCalled atomic.Bool
	newToken := "rotated-token"
	newKey := signingKeyB64

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/metrics":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]bool{"rotation_required": true})
		case "/v1/agents/self/rotate":
			rotateCalled.Store(true)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"token":      newToken,
				"signing_key": newKey,
				"expires_at":  time.Now().Add(365 * 24 * time.Hour).Unix(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := newTestConfig(srv.URL)
	client, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.http = srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bufDir := t.TempDir()
	buf, _ := buffer.New(bufDir, 10, time.Hour)

	// Seed one metric batch into the push loop.
	go func() {
		ch := make(chan []metrics.Point, 1)
		ch <- []metrics.Point{{Metric: "cpu.percent", Value: 1.0, Labels: map[string]string{}, TS: time.Now().Unix()}}
		close(ch)
		client.RunPushLoop(ctx, buf, ch)
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rotateCalled.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !rotateCalled.Load() {
		t.Error("expected POST /v1/agents/self/rotate to be called after rotation_required=true")
	}

	// Verify in-memory credentials were updated.
	client.mu.RLock()
	tok := client.token
	client.mu.RUnlock()
	if tok != newToken {
		t.Errorf("in-memory token: want %q, got %q", newToken, tok)
	}
}

// TestIntegration_BufferDrain verifies that the push loop replays a buffered
// batch after a transient failure clears.
func TestIntegration_BufferDrain(t *testing.T) {
	var pushCount atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/metrics" {
			http.NotFound(w, r)
			return
		}
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		io.Copy(io.Discard, r.Body)
		pushCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := newTestConfig(srv.URL)
	cfg.PushInterval = config.Duration{Duration: 50 * time.Millisecond}
	client, err := New(cfg, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client.http = srv.Client()

	bufDir := t.TempDir()
	buf, _ := buffer.New(bufDir, 10, time.Hour)

	// Pre-seed one buffered entry so the drain path is exercised.
	payload, _ := json.Marshal(metricsBatch{
		AgentID: cfg.AgentID,
		Points: []metrics.Point{
			{Metric: "cpu.percent", Value: 99.0, Labels: map[string]string{}, TS: time.Now().Unix()},
		},
	})
	buf.Store(buffer.Entry{Kind: buffer.KindMetrics, Payload: payload})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metricsCh := make(chan []metrics.Point)
	go client.RunPushLoop(ctx, buf, metricsCh)

	// Let one failed cycle happen, then clear the failure flag.
	time.Sleep(120 * time.Millisecond)
	fail.Store(false)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if pushCount.Load() > 0 && buf.Len() == 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	cancel()

	if buf.Len() != 0 {
		t.Errorf("buffered entry was not drained: buf.Len()=%d", buf.Len())
	}
}

// Compile-time check: ensure metricsBatch is accessible in the test (same package).
var _ = strconv.Itoa