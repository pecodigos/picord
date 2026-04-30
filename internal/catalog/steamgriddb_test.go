package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSteamGridDBClient_Search_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected bearer token, got %q", r.Header.Get("Authorization"))
		}
		if strings.HasSuffix(r.URL.Path, "/search/autocomplete/Hollow Knight") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"data":[{"id":5251636,"name":"Hollow Knight","release_date":0}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewSteamGridDBClient("test-key")
	client.BaseURL = ts.URL

	id, name, err := client.Search(context.Background(), "Hollow Knight")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 5251636 {
		t.Errorf("id = %d, want 5251636", id)
	}
	if name != "Hollow Knight" {
		t.Errorf("name = %q, want Hollow Knight", name)
	}
}

func TestSteamGridDBClient_Search_NoResults(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer ts.Close()

	client := NewSteamGridDBClient("test-key")
	client.BaseURL = ts.URL

	_, _, err := client.Search(context.Background(), "NoResults")
	if err == nil {
		t.Error("expected error for empty results")
	}
}

func TestSteamGridDBClient_Search_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := NewSteamGridDBClient("test-key")
	client.BaseURL = ts.URL

	_, _, err := client.Search(context.Background(), "Anything")
	if err == nil {
		t.Error("expected error for 401")
	}
}

func TestSteamGridDBClient_Search_EmptyKey(t *testing.T) {
	client := NewSteamGridDBClient("")
	_, _, err := client.Search(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "api key") {
		t.Errorf("expected api key error, got %v", err)
	}
}

func TestSteamGridDBClient_Search_EmptyTitle(t *testing.T) {
	client := NewSteamGridDBClient("key")
	_, _, err := client.Search(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty title error, got %v", err)
	}
}

func TestSteamGridDBClient_FindGrid(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/grids/game/123") {
			w.Write([]byte(`{"success":true,"data":[{"id":1,"url":"https://cdn2.steamgriddb.com/grid/abc.png","thumb":"https://cdn2.steamgriddb.com/grid/abc_thumb.png","width":600,"height":900,"style":"alternate","mime":"image/png"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewSteamGridDBClient("test-key")
	client.BaseURL = ts.URL

	url, w, h, err := client.FindGrid(context.Background(), 123, GridPreference{Style: "alternate"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://cdn2.steamgriddb.com/grid/abc.png" {
		t.Errorf("url = %q", url)
	}
	if w != 600 || h != 900 {
		t.Errorf("dimensions = %dx%d, want 600x900", w, h)
	}
}

func TestSteamGridDBClient_EnrichEntry(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/autocomplete/Celeste":
			w.Write([]byte(`{"success":true,"data":[{"id":5251694,"name":"Celeste","release_date":0}]}`))
		case "/grids/game/5251694":
			w.Write([]byte(`{"success":true,"data":[{"id":1,"url":"https://cdn2.steamgriddb.com/grid/abc.png","thumb":"https://cdn2.steamgriddb.com/grid/abc_thumb.png","width":600,"height":900,"style":"alternate","mime":"image/png"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	client := NewSteamGridDBClient("test-key")
	client.BaseURL = ts.URL

	url, title, err := client.EnrichEntry(context.Background(), "Celeste")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "Celeste" {
		t.Errorf("title = %q, want Celeste", title)
	}
	if url != "https://cdn2.steamgriddb.com/grid/abc.png" {
		t.Errorf("url = %q", url)
	}
}
