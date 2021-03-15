// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/experiment"
)

func TestSetAndLoadExperiments(t *testing.T) {
	ctx := context.Background()
	const testFeature = "test-feature"
	var mu sync.Mutex
	testExps := []*internal.Experiment{{Name: testFeature, Rollout: 100}}
	testGetter := func(context.Context) ([]*internal.Experiment, error) {
		mu.Lock()
		defer mu.Unlock()
		return testExps, nil
	}
	experimenter, err := NewExperimenter(ctx, 10*time.Millisecond, testGetter, nil)
	if err != nil {
		t.Fatal(err)
	}

	var featureIsOn bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if experiment.IsActive(r.Context(), testFeature) {
			featureIsOn = true
		} else {
			featureIsOn = false
		}
	})

	mux := http.NewServeMux()
	mux.Handle("/", Experiment(experimenter)(handler))
	ts := httptest.NewServer(mux)
	makeRequest := func(t *testing.T) {
		t.Helper()

		req, err := http.NewRequest("GET", ts.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-For", "1.2.3.4")

		if _, err := ts.Client().Do(req); err != nil {
			t.Fatal(err)
		}
	}

	makeRequest(t)
	if !featureIsOn {
		t.Fatalf("experiment %q should be active", testFeature)
	}

	mu.Lock()
	testExps = []*internal.Experiment{{Name: testFeature, Rollout: 0}}
	mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	makeRequest(t)
	if featureIsOn {
		t.Fatalf("experiment %q should not be active", testFeature)
	}
}

func TestShouldSetExperiment(t *testing.T) {
	ipv4Addr := func() string {
		a := make([]string, 4)
		for i := 0; i < 4; i++ {
			// The use case is simple enough that a deterministic
			// seed should provide enough coverage.
			a[i] = strconv.Itoa(rand.Intn(256))
		}
		return strings.Join(a, ".")
	}
	var ipAddresses []string
	const numIPs = 10000.0
	for i := 0; i < numIPs; i++ {
		ip := ipv4Addr()
		ipAddresses = append(ipAddresses, ip)
	}

	for _, rollout := range []uint{0, 33, 47, 50, 53, 75, 100} {
		t.Run(fmt.Sprintf("%d", rollout), func(t *testing.T) {
			test := &internal.Experiment{
				Name:    "test",
				Rollout: rollout,
			}
			var inExperiment int
			for _, ip := range ipAddresses {
				req, err := http.NewRequest("GET", "http://foo", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Add("X-Forwarded-For", ip)
				if shouldSetExperiment(req, test) {
					inExperiment++
				}
			}
			if test.Rollout == 0 {
				if inExperiment != 0 {
					t.Fatalf("rollout is 0 and inExperiment = %d; want = 0", inExperiment)
				}
				return
			}
			got := uint(100 * inExperiment / numIPs)
			if got != test.Rollout {
				t.Errorf("rollout = %d; want = %d", got, test.Rollout)
			}
		})
	}
}
