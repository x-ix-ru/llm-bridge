package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		resp := modelsResponse{
			Object: "list",
			Data: []json.RawMessage{
				json.RawMessage(`{"id":"gpt-3.5-turbo","object":"model","created":100,"owned_by":"test"}`),
				json.RawMessage(`{"id":"gpt-4","object":"model","created":200,"owned_by":"test"}`),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := New(1 * time.Minute)
	d.SetServers([]string{srv.URL})

	err := d.Discover(context.Background())
	require.NoError(t, err)

	models := d.Models()
	assert.Len(t, models, 2)
	assert.Contains(t, models, "gpt-3.5-turbo")
	assert.Contains(t, models, "gpt-4")
	assert.Equal(t, []string{srv.URL}, models["gpt-3.5-turbo"])
	assert.True(t, d.IsHealthy(srv.URL))

	details := d.ModelDetails()
	assert.Contains(t, details, "gpt-3.5-turbo")
	assert.Contains(t, details, "gpt-4")
	// Verify full metadata is preserved.
	var parsed struct {
		Created int `json:"created"`
	}
	json.Unmarshal(details["gpt-3.5-turbo"], &parsed)
	assert.Equal(t, 100, parsed.Created)
}

func TestModelsReturnsCopy(t *testing.T) {
	d := New(1 * time.Minute)
	d.mu.Lock()
	d.models["test-model"] = []string{"http://srv1:8000"}
	d.mu.Unlock()

	models := d.Models()
	models["test-model"] = append(models["test-model"], "http://srv2:8000")

	original := d.Models()
	assert.Len(t, original["test-model"], 1)
}

func TestHealthyReturnsCopy(t *testing.T) {
	d := New(1 * time.Minute)
	d.mu.Lock()
	d.healthy["http://srv1:8000"] = true
	d.mu.Unlock()

	healthy := d.Healthy()
	healthy["http://srv1:8000"] = false

	original := d.Healthy()
	assert.True(t, original["http://srv1:8000"])
}

func TestUnhealthyServerExcluded(t *testing.T) {
	d := New(1 * time.Minute)
	d.mu.Lock()
	d.models["test-model"] = []string{"http://healthy:8000", "http://unhealthy:8000"}
	d.healthy["http://healthy:8000"] = true
	d.healthy["http://unhealthy:8000"] = false
	d.mu.Unlock()

	servers := d.ServersForModel("test-model")
	assert.Len(t, servers, 1)
	assert.Equal(t, "http://healthy:8000", servers[0])
}

func TestSetServers(t *testing.T) {
	d := New(1 * time.Minute)
	d.SetServers([]string{"http://a:8000", "http://b:8000"})

	d.mu.RLock()
	assert.Len(t, d.servers, 2)
	d.mu.RUnlock()

	d.SetServers([]string{"http://c:8000"})

	d.mu.RLock()
	assert.Len(t, d.servers, 1)
	assert.Equal(t, "http://c:8000", d.servers[0])
	d.mu.RUnlock()
}

func TestStartStop(t *testing.T) {
	var mu sync.Mutex
	polled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		polled = true
		mu.Unlock()
		resp := modelsResponse{
			Object: "list",
			Data:   []json.RawMessage{json.RawMessage(`{"id":"test-model","object":"model"}`)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := New(50 * time.Millisecond)
	d.SetServers([]string{srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx)

	time.Sleep(120 * time.Millisecond)
	cancel()
	d.Stop()

	mu.Lock()
	assert.True(t, polled)
	mu.Unlock()

	models := d.Models()
	assert.Contains(t, models, "test-model")
}

func TestStartMultipleTimes(t *testing.T) {
	d := New(1 * time.Minute)
	ctx := context.Background()
	d.Start(ctx)
	d.Start(ctx)
	d.Stop()
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Second)
		resp := modelsResponse{
			Object: "list",
			Data:   []json.RawMessage{json.RawMessage(`{"id":"slow-model","object":"model"}`)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := New(1 * time.Minute)
	d.SetServers([]string{srv.URL})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := d.Discover(ctx)
	assert.Error(t, err)
	assert.False(t, d.IsHealthy(srv.URL))
}

func TestServerError(t *testing.T) {
	d := New(1 * time.Minute)
	d.SetServers([]string{"http://127.0.0.1:1"})

	err := d.Discover(context.Background())
	assert.NoError(t, err)
	assert.False(t, d.IsHealthy("http://127.0.0.1:1"))
}

func TestHealthTracking(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := modelsResponse{
			Object: "list",
			Data:   []json.RawMessage{json.RawMessage(`{"id":"test","object":"model"}`)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := New(1 * time.Minute)
	d.SetServers([]string{srv.URL})

	err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.False(t, d.IsHealthy(srv.URL))
	assert.Empty(t, d.Models())

	err = d.Discover(context.Background())
	require.NoError(t, err)
	assert.True(t, d.IsHealthy(srv.URL))
	assert.Contains(t, d.Models(), "test")
}

func TestServersForModelNonexistent(t *testing.T) {
	d := New(1 * time.Minute)
	servers := d.ServersForModel("nonexistent")
	assert.Nil(t, servers)
}

func TestServersForModelAllUnhealthy(t *testing.T) {
	d := New(1 * time.Minute)
	d.mu.Lock()
	d.models["test-model"] = []string{"http://srv1:8000", "http://srv2:8000"}
	d.healthy["http://srv1:8000"] = false
	d.healthy["http://srv2:8000"] = false
	d.mu.Unlock()

	servers := d.ServersForModel("test-model")
	assert.Empty(t, servers)
}

func TestModelsAfterServerRemovesModel(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp modelsResponse
		if callCount == 1 {
			resp = modelsResponse{
				Object: "list",
				Data:   []json.RawMessage{json.RawMessage(`{"id":"model-a","object":"model"}`), json.RawMessage(`{"id":"model-b","object":"model"}`)},
			}
		} else {
			resp = modelsResponse{
				Object: "list",
				Data:   []json.RawMessage{json.RawMessage(`{"id":"model-a","object":"model"}`)},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	d := New(1 * time.Minute)
	d.SetServers([]string{srv.URL})

	require.NoError(t, d.Discover(context.Background()))
	assert.Contains(t, d.Models(), "model-a")
	assert.Contains(t, d.Models(), "model-b")

	require.NoError(t, d.Discover(context.Background()))
	assert.Contains(t, d.Models(), "model-a")
	assert.NotContains(t, d.Models(), "model-b")
}

func TestMultipleServers(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := modelsResponse{
			Object: "list",
			Data:   []json.RawMessage{json.RawMessage(`{"id":"model-a","object":"model"}`)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := modelsResponse{
			Object: "list",
			Data:   []json.RawMessage{json.RawMessage(`{"id":"model-a","object":"model"}`), json.RawMessage(`{"id":"model-b","object":"model"}`)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv2.Close()

	d := New(1 * time.Minute)
	d.SetServers([]string{srv1.URL, srv2.URL})

	require.NoError(t, d.Discover(context.Background()))

	models := d.Models()
	assert.Len(t, models["model-a"], 2)
	assert.Contains(t, models["model-a"], srv1.URL)
	assert.Contains(t, models["model-a"], srv2.URL)
	assert.Len(t, models["model-b"], 1)
	assert.Equal(t, []string{srv2.URL}, models["model-b"])
}

func TestIsHealthy(t *testing.T) {
	d := New(1 * time.Minute)
	assert.False(t, d.IsHealthy("http://unknown:8000"))

	d.mu.Lock()
	d.healthy["http://known:8000"] = true
	d.mu.Unlock()

	assert.True(t, d.IsHealthy("http://known:8000"))
}
