package steamworkshop

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"hearth/internal/panel"
)

func TestParseReferenceAcceptsIDAndOfficialDetailURL(t *testing.T) {
	for _, input := range []string{
		"3625223587",
		"https://steamcommunity.com/sharedfiles/filedetails/?id=3625223587",
		"http://steamcommunity.com/workshop/filedetails?id=3625223587&searchtext=test",
	} {
		id, err := ParseReference(input)
		if err != nil || id != "3625223587" {
			t.Fatalf("ParseReference(%q) = %q, %v", input, id, err)
		}
	}
	for _, input := range []string{"", "0", "../1", "https://example.com/sharedfiles/filedetails/?id=1"} {
		if _, err := ParseReference(input); err == nil {
			t.Fatalf("ParseReference(%q) succeeded", input)
		}
	}
}

func TestLookupReturnsBoundedPalworldMetadata(t *testing.T) {
	client := newClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if r.Method != http.MethodPost || form.Get("publishedfileids[0]") != "3625223587" {
			t.Fatalf("request = %s %#v", r.Method, form)
		}
		return jsonResponse(`{"response":{"publishedfiledetails":[{"publishedfileid":"3625223587","result":1,"creator":"7656119","title":"UE4SS Experimental (Palworld)","file_size":"17037341","time_updated":1784446590,"consumer_app_id":1623730}]}}`), nil
	})}, "https://api.example.invalid/details")
	item, err := client.Lookup(context.Background(), "1623730", "3625223587")
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "3625223587" || item.Title != "UE4SS Experimental (Palworld)" || item.FileSize != 17037341 ||
		item.AppID != "1623730" || !strings.Contains(item.WorkshopURL, item.ID) {
		t.Fatalf("item = %#v", item)
	}
}

func TestLookupRejectsAnotherGame(t *testing.T) {
	client := newClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"response":{"publishedfiledetails":[{"publishedfileid":"3625223587","result":1,"title":"Other","file_size":"1","time_updated":1,"consumer_app_id":123}]}}`), nil
	})}, "https://api.example.invalid/details")
	_, err := client.Lookup(context.Background(), "1623730", "3625223587")
	if err == nil || !strings.Contains(err.Error(), "不属于 Palworld") || !strings.Contains(err.Error(), panel.ErrInvalid.Error()) {
		t.Fatalf("error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
