package forward

import (
	"fmt"
	"testing"
)

func BenchmarkHTTPMatch(b *testing.B) {
	c := &HTTPConfig{}
	for i := 0; i < 100; i++ {
		s := ServerBlock{ID: fmt.Sprint("site", i), Hosts: []string{fmt.Sprintf("site%d.example.com", i)}}
		for j := 0; j < 10; j++ {
			s.Locations = append(s.Locations, Location{ID: fmt.Sprintf("route-%d-%d", i, j), Match: "prefix", Path: fmt.Sprintf("/api/%d/", j), Scheme: "http", Host: "127.0.0.1", Port: 80})
		}
		c.Servers = append(c.Servers, s)
	}
	r, err := Compile(c)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l, _ := r.Match("site99.example.com", "/api/9/items")
		if l == nil {
			b.Fatal("no route")
		}
	}
}
