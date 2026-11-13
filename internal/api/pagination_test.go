package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/maymeow/fjo/internal/fjoerr"
)

func TestParsePage(t *testing.T) {
	h := http.Header{}
	h.Set("X-Total-Count", "42")
	h.Add("Link", `<https://h/api/v1/x?page=2&limit=10>; rel="next",<https://h/api/v1/x?page=5&limit=10>; rel="last"`)
	p := ParsePage(h)
	if p.Total != 42 || p.Next != "https://h/api/v1/x?page=2&limit=10" || p.Last != "https://h/api/v1/x?page=5&limit=10" {
		t.Errorf("page = %+v", p)
	}
	if p := ParsePage(http.Header{}); p.Total != -1 || p.Next != "" {
		t.Errorf("empty page = %+v", p)
	}
	h = http.Header{"Link": {`garbage, <x>; rel="prev", <y>; rel="first next"`}}
	if p := ParsePage(h); p.Next != "y" {
		t.Errorf("multi-rel next = %+v", p)
	}
}

// pagedServer serves items 1..total in pages of the requested limit.
func pagedServer(t *testing.T, total int, linkHost func(r *http.Request) string) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 || limit > 3 {
			limit = 3 // server-side MAX_RESPONSE_ITEMS
		}
		from := (page - 1) * limit
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
		if from+limit < total {
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/items?page=%d&limit=%d>; rel="next"`, linkHost(r), page+1, limit))
		}
		fmt.Fprint(w, "[")
		for i := from; i < from+limit && i < total; i++ {
			if i > from {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"n":%d}`, i+1)
		}
		fmt.Fprint(w, "]")
	}))
}

type item struct {
	N int `json:"n"`
}

func TestListAllAndLimit(t *testing.T) {
	var srv *httptest.Server
	srv = pagedServer(t, 8, func(*http.Request) string { return srv.URL })
	defer srv.Close()
	c := newTestClient(t, srv, "/api/v1")

	all, err := List[item](context.Background(), c, "items", nil, 0)
	if err != nil || len(all) != 8 || all[7].N != 8 {
		t.Fatalf("all = %v, %v", all, err)
	}
	some, err := List[item](context.Background(), c, "items", nil, 5)
	if err != nil || len(some) != 5 || some[4].N != 5 {
		t.Fatalf("limited = %v, %v", some, err)
	}
}

func TestPagesRefusesForeignNextLink(t *testing.T) {
	srv := pagedServer(t, 8, func(*http.Request) string { return "https://evil.example.com" })
	defer srv.Close()
	_, err := List[item](context.Background(), newTestClient(t, srv, "/api/v1"), "items", nil, 0)
	if err == nil || fjoerr.KindOf(err) != fjoerr.KindGeneral {
		t.Fatalf("err = %v", err)
	}
}

func TestPagesDetectsLoop(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/api/v1/items?page=1>; rel="next"`, srv.URL))
		fmt.Fprint(w, `[{"n":1}]`)
	}))
	defer srv.Close()
	_, err := List[item](context.Background(), newTestClient(t, srv, "/api/v1"), "items?page=1", nil, 0)
	if err == nil {
		t.Fatal("expected loop error")
	}
}

func TestPagesMaxPages(t *testing.T) {
	var srv *httptest.Server
	srv = pagedServer(t, 100, func(*http.Request) string { return srv.URL })
	defer srv.Close()
	n := 0
	err := newTestClient(t, srv, "/api/v1").Pages(context.Background(), "items", nil, 2, func(*http.Response) (bool, error) {
		n++
		return false, nil
	})
	if err != nil || n != 2 {
		t.Errorf("pages = %d, err = %v", n, err)
	}
}
