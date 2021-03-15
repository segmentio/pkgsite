// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package frontend

import (
	"encoding/json"
	"go/format"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/pkgsite/internal/log"
)

// playgroundURL is the playground endpoint used for share links.
const playgroundURL = "https://play.golang.org"

var (
	keyPlaygroundShareStatus = tag.MustNewKey("playground.share.status")
	playgroundShareStatus    = stats.Int64(
		"go-discovery/playground_share_count",
		"The status of a request to play.golang.org/share",
		stats.UnitDimensionless,
	)

	PlaygroundShareRequestCount = &view.View{
		Name:        "go-discovery/playground/share_count",
		Measure:     playgroundShareStatus,
		Aggregation: view.Count(),
		Description: "Playground share request count",
		TagKeys:     []tag.Key{keyPlaygroundShareStatus},
	}
)

// handlePlay handles requests that mirror play.golang.org/share.
func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	makeFetchPlayRequest(w, r, playgroundURL)
}

func httpErrorStatus(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func makeFetchPlayRequest(w http.ResponseWriter, r *http.Request, pgURL string) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		httpErrorStatus(w, http.StatusMethodNotAllowed)
		return
	}
	req, err := http.NewRequest("POST", pgURL+"/share", r.Body)
	if err != nil {
		log.Errorf(ctx, "ERROR share error: %v", err)
		httpErrorStatus(w, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req = req.WithContext(r.Context())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Errorf(ctx, "ERROR share error: %v", err)
		httpErrorStatus(w, http.StatusInternalServerError)
		return
	}
	stats.RecordWithTags(r.Context(),
		[]tag.Mutator{tag.Upsert(keyPlaygroundShareStatus, strconv.Itoa(resp.StatusCode))},
		playgroundShareStatus.M(int64(resp.StatusCode)),
	)
	copyHeader := func(k string) {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	copyHeader("Content-Type")
	copyHeader("Content-Length")
	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Errorf(ctx, "ERROR writing shareId: %v", err)
	}
}

// proxyPlayground is a handler that proxies playground requests to play.golang.org.
func (s *Server) proxyPlayground(w http.ResponseWriter, r *http.Request) {
	makePlaygroundProxy().ServeHTTP(w, r)
}

// makePlaygroundProxy creates a proxy that sends requests to play.golang.org.
// The prefix /play is removed from the URL path.
func makePlaygroundProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			originHost := "play.golang.org"
			req.Header.Add("X-Forwarded-Host", req.Host)
			req.Header.Add("X-Origin-Host", originHost)
			req.Host = originHost
			req.URL.Scheme = "https"
			req.URL.Host = originHost
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/play")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Errorf(r.Context(), "ERROR playground proxy error: %v", err)
			httpErrorStatus(w, http.StatusInternalServerError)
		},
	}
}

type fmtResponse struct {
	Body  string
	Error string
}

// fmtHandler takes a Go program in its "body" form value, formats it with
// standard gofmt formatting, and writes a fmtResponse as a JSON object.
func (s *Server) handleFmt(w http.ResponseWriter, r *http.Request) {
	resp := new(fmtResponse)
	body, err := format.Source([]byte(r.FormValue("body")))
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Body = string(body)
	}
	w.Header().Set("Content-type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}
