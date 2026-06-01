package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePrometheus_Empty(t *testing.T) {
	m := parsePrometheus("")
	assert.True(t, m.IsZero())
}

func TestParsePrometheus_Full(t *testing.T) {
	body := `# HELP vllm:num_requests_running Number of requests currently running
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{engine="0",model_name="smart"} 3
# HELP vllm:num_requests_waiting Number of requests waiting
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting{engine="0",model_name="smart"} 2
# HELP vllm:kv_cache_usage_perc KV-cache usage
# TYPE vllm:kv_cache_usage_perc gauge
vllm:kv_cache_usage_perc{engine="0",model_name="smart"} 0.425
# HELP vllm:prompt_tokens_total Total prompt tokens
# TYPE vllm:prompt_tokens_total counter
vllm:prompt_tokens_total{engine="0",model_name="smart"} 1500
# HELP vllm:generation_tokens_total Total generation tokens
# TYPE vllm:generation_tokens_total counter
vllm:generation_tokens_total{engine="0",model_name="smart"} 750
`

	m := parsePrometheus(body)
	assert.Equal(t, 3, m.RequestsRunning)
	assert.Equal(t, 2, m.RequestsWaiting)
	assert.Equal(t, 42.5, m.KVCacheUsagePerc) // 0.425 * 100
	assert.Equal(t, int64(1500), m.PromptTokensTotal)
	assert.Equal(t, int64(750), m.GenTokensTotal)
}

func TestParsePrometheus_LabelsStripped(t *testing.T) {
	// Same metric name with different labels should all match.
	body := `vllm:num_requests_running{engine="0",model_name="foo"} 1
vllm:num_requests_running{engine="0",model_name="bar"} 2
`
	m := parsePrometheus(body)
	// Last value wins (as with any duplicate metric line).
	assert.Equal(t, 2, m.RequestsRunning)
}

func TestParsePrometheus_Partial(t *testing.T) {
	body := `vllm:num_requests_running 5
vllm:kv_cache_usage_perc 0.881
`

	m := parsePrometheus(body)
	assert.Equal(t, 5, m.RequestsRunning)
	assert.Equal(t, 0, m.RequestsWaiting)
	assert.Equal(t, 88.1, m.KVCacheUsagePerc) // 0.881 * 100
	assert.Equal(t, int64(0), m.PromptTokensTotal)
	assert.Equal(t, int64(0), m.GenTokensTotal)
}

func TestParsePrometheus_NonNumeric(t *testing.T) {
	body := `vllm:num_requests_running not_a_number`
	m := parsePrometheus(body)
	assert.True(t, m.IsZero())
}

func TestParsePrometheus_UnknownMetricsIgnored(t *testing.T) {
	body := `vllm:num_requests_running 1
some_other_metric 42
vllm:unknown_thing 3
`

	m := parsePrometheus(body)
	assert.Equal(t, 1, m.RequestsRunning)
	assert.Equal(t, 0, m.RequestsWaiting)
}

func TestStripLabels_NoLabels(t *testing.T) {
	assert.Equal(t, "vllm:num_requests_running", stripLabels("vllm:num_requests_running"))
}

func TestStripLabels_WithLabels(t *testing.T) {
	assert.Equal(t, "vllm:num_requests_running", stripLabels(`vllm:num_requests_running{engine="0",model_name="smart"}`))
}

func TestStripLabels_EmptyLabel(t *testing.T) {
	assert.Equal(t, "foo", stripLabels("foo{}"))
}

func TestCollector_GetSetRemove(t *testing.T) {
	c := New(10 * time.Second)
	defer c.Stop()

	m := c.Get("http://nonexistent")
	assert.Nil(t, m)

	c.SetServers([]string{"http://s1:8000", "http://s2:8000"})
	assert.NotNil(t, c.Get("http://s1:8000"))
	assert.True(t, c.Get("http://s1:8000").IsZero())

	c.Remove("http://s1:8000")
	assert.Nil(t, c.Get("http://s1:8000"))
}

func TestCollector_FetchOne_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/metrics", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`vllm:num_requests_running{engine="0"} 7
vllm:kv_cache_usage_perc{engine="0"} 0.555
`))
	}))
	defer ts.Close()

	c := New(time.Hour)
	m, err := c.fetchOne(context.Background(), ts.URL)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, 7, m.RequestsRunning)
	assert.InDelta(t, 55.5, m.KVCacheUsagePerc, 0.001) // 0.555 * 100
	assert.False(t, m.UpdatedAt.IsZero())
}

func TestCollector_FetchOne_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := New(time.Hour)
	m, err := c.fetchOne(context.Background(), ts.URL)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.True(t, m.IsZero())
}

func TestCollector_FetchOne_ConnectionError(t *testing.T) {
	c := New(time.Hour)
	// Trying to connect to a closed port should fail.
	_, err := c.fetchOne(context.Background(), "http://127.0.0.1:1")
	assert.Error(t, err)
}

func TestCollector_Start_And_GetAll(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`vllm:num_requests_running 2`))
	}))
	defer ts.Close()

	c := New(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.Start(ctx, []string{ts.URL})

	all := c.GetAll()
	require.Contains(t, all, ts.URL)
	assert.Equal(t, 2, all[ts.URL].RequestsRunning)
}

func TestCollector_ContextCancellation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang until context is cancelled.
		<-r.Context().Done()
	}))
	defer ts.Close()

	// Use a short timeout to trigger context cancellation.
	c := New(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// fetchOne should return a context error.
	_, err := c.fetchOne(ctx, ts.URL)
	assert.Error(t, err)
}
