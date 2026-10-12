package forward

import "testing"

func TestHTTPMatchOrder(t *testing.T) {
	c := &HTTPConfig{Servers: []ServerBlock{{ID: "s", Hosts: []string{"example.com", "*.example.org"}, Locations: []Location{
		{ID: "root", Match: "prefix", Path: "/", Scheme: "http", Host: "localhost", Port: 80},
		{ID: "api", Match: "^~", Path: "/api/", Scheme: "http", Host: "localhost", Port: 80},
		{ID: "exact", Match: "=", Path: "/api/exact", Scheme: "http", Host: "localhost", Port: 80},
		{ID: "regex", Match: "~*", Path: "[.]jpg$", Scheme: "http", Host: "localhost", Port: 80},
	}}, {ID: "default", Hosts: []string{"_"}, Locations: []Location{{ID: "fallback", Path: "/", Scheme: "http", Host: "localhost", Port: 80}}}}}
	r, err := Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ host, path, id string }{{"example.com:8080", "/api/exact", "exact"}, {"EXAMPLE.COM", "/api/a.jpg", "api"}, {"x.example.org", "/a.JPG", "regex"}, {"example.com", "/other", "root"}, {"unknown", "/", "fallback"}} {
		l, _ := r.Match(test.host, test.path)
		if l == nil || l.ID != test.id {
			t.Fatalf("%+v: got %+v", test, l)
		}
	}
	c.Servers[0].Locations[3].Path = "(?=x)"
	if _, err := Compile(c); err == nil {
		t.Fatal("accepted invalid RE2 expression")
	}
}
