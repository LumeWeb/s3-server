package handlers

import (
	"net"
	"net/http"
	"strings"
)

// RootToPanelRedirect wraps the S3 handler so that an anonymous GET to the
// server root ("/") is redirected to the panel, while all genuine S3 requests
// are passed through untouched.
//
// A root-level GET is the S3 ListBuckets operation, which requires SigV4
// authentication (it returns AccessDenied for anonymous requests). Browsers
// never sign requests, so an unauthenticated GET / can only be a user
// navigating to the server URL. Real S3 clients always supply an auth marker,
// so none of them are affected by the redirect.
func RootToPanelRedirect(next http.Handler, hostBases []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" && !isS3RootRequest(r, hostBases) {
			http.Redirect(w, r, "/_panel/", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isS3RootRequest reports whether a bare GET / should be treated as an S3 API
// request rather than a browser navigation:
//   - authenticated requests (SigV4 Authorization header)
//   - presigned requests (X-Amz-* query parameters)
//   - virtual-host style requests addressed to a bucket subdomain
//     ({bucket}.{hostBase}), which serve bucket object listings at "/".
func isS3RootRequest(r *http.Request, hostBases []string) bool {
	if r.Header.Get("Authorization") != "" {
		return true
	}
	for key := range r.URL.Query() {
		if strings.HasPrefix(key, "X-Amz-") {
			return true
		}
	}
	return hostMatchesBucketBase(r.Host, hostBases)
}

// hostMatchesBucketBase reports whether host addresses a bucket as a subdomain
// of one of the configured host bucket bases, mirroring the bucket-from-host
// logic used by the S3 handler.
func hostMatchesBucketBase(host string, hostBases []string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, base := range hostBases {
		suffix := "." + strings.Trim(base, ".")
		if !strings.HasSuffix(host, suffix) {
			continue
		}
		bucket := host[:len(host)-len(suffix)]
		if bucket != "" && !strings.Contains(bucket, ".") {
			return true
		}
	}
	return false
}
