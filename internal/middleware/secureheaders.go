// Copyright 2019-2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package middleware

import (
	"fmt"
	"net/http"
	"strings"
)

var scriptHashes = []string{
	// From content/static/html/base.tmpl
	"'sha256-d6W7MwuGWbguTHRzQhf5QN1jXmNo9Ao218saZkWLWZI='",
	"'sha256-qPGTOKPn+niRiNKQIEX0Ktwuj+D+iPQWIxnlhPicw58='",
	"'sha256-CgM7SjnSbDyuIteS+D1CQuSnzyKwL0qtXLU6ZW2hB+g='",
	// From content/static/html/pages/badge.tmpl
	"'sha256-T7xOt6cgLji3rhOWyKK7t5XKv8+LASQwOnHiHHy8Kwk='",
	// From content/static/html/pages/details.tmpl
	"'sha256-EWdCQW4XtY7zS2MZgs76+2EhMbqpaPtC+9EPGnbHBtM='",
	// From content/static/html/pages/fetch.tmpl
	"'sha256-1J6DWwTWs/QDZ2+ORDuUQCibmFnXXaNXYOtc0Jk6VU4='",
	// From content/static/html/pages/pkg_doc.tmpl
	"'sha256-91GG/273d2LdEV//lJMbTodGN501OuKZKYYphui+wDQ='",
	"'sha256-Y1vZzPZ448awUtFwK5f2nES8NyyeM5dgiQ/E3klx4GM='",
	"'sha256-gBtJYPzfgw/0FIACORDIAD08i5rxTQ5J0rhIU656A2U='",
	// From content/static/html/worker/index.tmpl
	"'sha256-5EpitFYSzGNQNUsqi5gAaLqnI3ZWfcRo/6gLTO0oCoE='",
}

// SecureHeaders adds a content-security-policy and other security-related
// headers to all responses.
func SecureHeaders() Middleware {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			csp := []string{
				// Disallow plugin content: pkg.go.dev does not use it.
				"object-src 'none'",
				// Disallow <base> URIs, which prevents attackers from changing the
				// locations of scripts loaded from relative URLs. The site doesn’t have
				// a <base> tag anyway.
				"base-uri 'none'",
				fmt.Sprintf("script-src 'unsafe-inline' 'strict-dynamic' https: http: %s",
					strings.Join(scriptHashes, " ")),
			}
			w.Header().Set("Content-Security-Policy", strings.Join(csp, "; "))
			// Don't allow frame embedding.
			w.Header().Set("X-Frame-Options", "deny")
			// Prevent MIME sniffing.
			w.Header().Set("X-Content-Type-Options", "nosniff")

			h.ServeHTTP(w, r)
		})
	}
}
