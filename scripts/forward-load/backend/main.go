// The load fixture is intentionally not part of the production server/Agent.
package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

func main() {
	payload := make([]byte, 64*1024)
	for i := range payload {
		payload[i] = 'r'
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := sha256.New()
		if _, err := io.Copy(hash, r.Body); err != nil {
			http.Error(w, "upload failed", 400)
			return
		}
		size, err := strconv.Atoi(r.URL.Query().Get("size"))
		if err != nil || size < 1 || size > len(payload) {
			http.Error(w, "invalid size", 400)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(size))
		w.Header().Set("X-Body-SHA256", fmt.Sprintf("%x", hash.Sum(nil)))
		w.Header().Set("X-Observed-Path", r.URL.RequestURI())
		w.WriteHeader(201)
		_, _ = w.Write(payload[:size])
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:19090", handler))
}
