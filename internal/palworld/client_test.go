package palworld

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRESTClientUsesBasicAuthAndOfficialPaths(t *testing.T) {
	requests := make(map[string]int)
	client, err := newRESTClient("http://127.0.0.1:8212", "", func() (string, error) { return "secret", nil })
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "admin" || password != "secret" {
			return testResponse(http.StatusUnauthorized, ""), nil
		}
		requests[request.URL.Path]++
		switch request.URL.Path {
		case "/v1/api/info":
			return jsonResponse(serverInfo{Version: "1.0.1"}), nil
		case "/v1/api/metrics":
			return jsonResponse(serverMetrics{CurrentPlayerNum: 2, MaxPlayerNum: 4}), nil
		case "/v1/api/players":
			return jsonResponse(serverPlayers{Players: []serverPlayer{{Name: "PalUser"}}}), nil
		case "/v1/api/save", "/v1/api/shutdown":
			return testResponse(http.StatusOK, ""), nil
		default:
			return testResponse(http.StatusNotFound, ""), nil
		}
	})
	ctx := context.Background()
	if _, err := client.info(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.metrics(ctx); err != nil {
		t.Fatal(err)
	}
	if players, err := client.players(ctx); err != nil || len(players.Players) != 1 {
		t.Fatalf("players() = %#v, %v", players, err)
	}
	if err := client.save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.shutdown(ctx, 30, "maintenance"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/v1/api/info", "/v1/api/metrics", "/v1/api/players", "/v1/api/save", "/v1/api/shutdown",
	} {
		if requests[path] != 1 {
			t.Fatalf("requests[%q] = %d", path, requests[path])
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(value any) *http.Response {
	var content strings.Builder
	_ = json.NewEncoder(&content).Encode(value)
	return testResponse(http.StatusOK, content.String())
}

func testResponse(status int, content string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(content)),
	}
}

func TestRESTClientRejectsNonLoopbackEndpoint(t *testing.T) {
	_, err := newRESTClient("http://192.0.2.10:8212", "admin", func() (string, error) {
		return "secret", nil
	})
	if err == nil {
		t.Fatal("newRESTClient() expected loopback validation error")
	}
}
