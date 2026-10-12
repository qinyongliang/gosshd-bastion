package forward

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// These fixtures exercise the complete HTTP proxy and asynchronous SQLite writer.
// Benchmarks include requests, body copies and accounting; Router.Match is not
// used as a proxy-throughput estimate.
func loadRuntime(tb testing.TB, body []byte) (*Runtime, *Logs, *http.Client, string) {
	tb.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		input, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body read", 500)
			return
		}
		w.Header().Set("X-Body-SHA256", fmt.Sprintf("%x", sha256.Sum256(input)))
		w.Header().Set("X-Observed-Path", r.URL.RequestURI())
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	}))
	tb.Cleanup(upstream.Close)
	logs, err := OpenLogs(tb.TempDir(), uuid.NewString(), 7)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = logs.Close() })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	config := &HTTPConfig{Servers: []ServerBlock{{ID: "site", Hosts: []string{"load.test"}, Locations: []Location{
		{ID: "api", Match: "^~", Path: "/api/", Scheme: "http", Host: "127.0.0.1", Port: upstream.Listener.Addr().(*net.TCPAddr).Port, StripPrefix: true},
	}}}}
	runtime, err := Start(context.Background(), listener, config, nil, func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", upstream.Listener.Addr().String())
	}, logs, nil)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = runtime.Close() })
	tr := &http.Transport{Proxy: nil, MaxIdleConns: 512, MaxIdleConnsPerHost: 512, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: tr, Timeout: 20 * time.Second}
	tb.Cleanup(tr.CloseIdleConnections)
	return runtime, logs, client, "http://" + listener.Addr().String()
}
func loadRequest(client *http.Client, base string, index int, input, expected []byte) error {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/request/%d?q=load", base, index), bytes.NewReader(input))
	if err != nil {
		return err
	}
	req.Host = "load.test"
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("request %d response body (%d bytes read): %w", index, len(body), err)
	}
	if response.StatusCode != 201 || !bytes.Equal(body, expected) || response.Header.Get("X-Body-SHA256") != fmt.Sprintf("%x", sha256.Sum256(input)) || response.Header.Get("X-Observed-Path") != fmt.Sprintf("/request/%d?q=load", index) {
		return fmt.Errorf("request %d: wrong status/body/hash/path: %d %q", index, response.StatusCode, response.Header.Get("X-Observed-Path"))
	}
	return nil
}
func TestHTTPForwardConcurrentAccounting(t *testing.T) {
	const workers = 64
	const requests = 1024
	input := bytes.Repeat([]byte("upload"), 128)
	output := bytes.Repeat([]byte("response"), 2048)
	runtime, logs, client, base := loadRuntime(t, output)
	stopRead := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			select {
			case <-stopRead:
				return
			default:
				_ = runtime.State()
				_, _ = logs.Stats(context.Background())
				time.Sleep(time.Millisecond)
			}
		}
	}()
	errors := make(chan error, requests)
	jobs := make(chan int)
	var wg sync.WaitGroup
	started := time.Now()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if err := loadRequest(client, base, index, input, output); err != nil {
					errors <- err
				}
			}
		}()
	}
	for index := range requests {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	close(stopRead)
	<-readDone
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	runtime.Close()
	if err := logs.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := logs.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Requests != requests || stats.Up != int64(requests*len(input)) || stats.Down != int64(requests*len(output)) || stats.Errors != 0 || stats.Dropped != 0 || stats.WriteFailures != 0 || runtime.State().Active != 0 {
		t.Fatalf("accounting mismatch %+v state=%+v", stats, runtime.State())
	}
	seen := map[string]bool{}
	var before int64
	for {
		events, err := logs.Query(context.Background(), LogFilter{Before: before, Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			if seen[event.ID] || event.RouteID != "api" || event.ServerID != "site" || event.Status != 201 || event.SourceIP != "127.0.0.1" {
				t.Fatalf("duplicate/wrong access event %+v", event)
			}
			seen[event.ID] = true
		}
		before = events[len(events)-1].Sequence
	}
	if len(seen) != requests {
		t.Fatalf("%d access rows, want %d", len(seen), requests)
	}
	t.Logf("concurrency=%d requests=%d response_bytes=%d elapsed=%s requests_per_sec=%.1f peak=%d dropped=%d", workers, requests, len(output), time.Since(started), float64(requests)/time.Since(started).Seconds(), stats.Peak, stats.Dropped)
}
func BenchmarkHTTPForwardDataPlane(b *testing.B) {
	for _, size := range []int{1024, 64 * 1024} {
		for _, workers := range []int{1, 16, 64} {
			b.Run(fmt.Sprintf("bytes=%d/concurrency=%d", size, workers), func(b *testing.B) {
				input := bytes.Repeat([]byte("u"), 256)
				body := bytes.Repeat([]byte("r"), size)
				runtime, logs, client, base := loadRuntime(b, body)
				jobs := make(chan int, workers)
				errs := make(chan error, workers)
				var wg sync.WaitGroup
				for range workers {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for index := range jobs {
							if err := loadRequest(client, base, index, input, body); err != nil {
								select {
								case errs <- err:
								default:
								}
							}
						}
					}()
				}
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; index < b.N; index++ {
					jobs <- index
				}
				close(jobs)
				wg.Wait()
				b.StopTimer()
				close(errs)
				for err := range errs {
					b.Fatal(err)
				}
				runtime.Close()
				if err := logs.Flush(context.Background()); err != nil {
					b.Fatal(err)
				}
				stats, err := logs.Stats(context.Background())
				if err != nil || stats.Requests != int64(b.N) {
					b.Fatalf("accounting %+v %v", stats, err)
				}
				b.ReportMetric(float64(stats.Dropped), "access-dropped")
				b.ReportMetric(float64(stats.WriteFailures), "write-failures")
				b.ReportMetric(float64(stats.Peak), "peak-active")
			})
		}
	}
}
