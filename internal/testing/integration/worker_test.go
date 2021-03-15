// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/google/safehtml/template"
	"golang.org/x/pkgsite/internal/cache"
	"golang.org/x/pkgsite/internal/config"
	"golang.org/x/pkgsite/internal/index"
	"golang.org/x/pkgsite/internal/proxy"
	"golang.org/x/pkgsite/internal/queue"
	"golang.org/x/pkgsite/internal/source"
	"golang.org/x/pkgsite/internal/worker"
)

func setupWorker(ctx context.Context, t *testing.T, proxyClient *proxy.Client, indexClient *index.Client,
	redisCacheClient, redisHAClient *redis.Client) (*httptest.Server, *worker.Fetcher, *queue.InMemory) {
	t.Helper()

	fetcher := &worker.Fetcher{
		ProxyClient:  proxyClient,
		SourceClient: source.NewClient(1 * time.Second),
		DB:           testDB,
		Cache:        cache.New(redisCacheClient),
	}
	// TODO: it would be better if InMemory made http requests
	// back to worker, rather than calling fetch itself.
	queue := queue.NewInMemory(ctx, 10, nil, func(ctx context.Context, mpath, version string) (int, error) {
		code, _, err := fetcher.FetchAndUpdateState(ctx, mpath, version, "test")
		return code, err
	})

	workerServer, err := worker.NewServer(&config.Config{}, worker.ServerConfig{
		DB:               testDB,
		IndexClient:      indexClient,
		ProxyClient:      proxyClient,
		SourceClient:     source.NewClient(1 * time.Second),
		RedisHAClient:    redisHAClient,
		RedisCacheClient: redisCacheClient,
		Queue:            queue,
		StaticPath:       template.TrustedSourceFromConstant("../../../content/static"),
	})
	if err != nil {
		t.Fatal(err)
	}
	workerMux := http.NewServeMux()
	workerServer.Install(workerMux.Handle)
	return httptest.NewServer(workerMux), fetcher, queue
}

func newRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	redisCache, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	return redis.NewClient(&redis.Options{Addr: redisCache.Addr()}), redisCache.Close
}
