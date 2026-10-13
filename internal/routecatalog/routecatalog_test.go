package routecatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMuxRecordsMethodRoutesWithoutExposingMutableState(t *testing.T) {
	t.Parallel()

	mux := New()
	mux.HandleFunc("GET /first", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("POST /second/{id}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("/ordinary", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	want := []Route{{Method: "GET", Path: "/first"}, {Method: "POST", Path: "/second/{id}"}}
	if got := mux.Routes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("routes = %#v, want %#v", got, want)
	}

	got := mux.Routes()
	got[0].Path = "/changed"

	if current := mux.Routes(); !reflect.DeepEqual(current, want) {
		t.Fatalf("returned route slice mutated catalog: %#v", current)
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ordinary", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("ordinary route status = %d", response.Code)
	}
}
