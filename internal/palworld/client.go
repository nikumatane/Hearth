package palworld

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type serverInfo struct {
	Version     string `json:"version"`
	ServerName  string `json:"servername"`
	Description string `json:"description"`
	WorldGUID   string `json:"worldguid"`
}

type serverMetrics struct {
	ServerFPS        int     `json:"serverfps"`
	CurrentPlayerNum int     `json:"currentplayernum"`
	ServerFrameTime  float64 `json:"serverframetime"`
	MaxPlayerNum     int     `json:"maxplayernum"`
	Uptime           int64   `json:"uptime"`
	BaseCampNum      int     `json:"basecampnum"`
	Days             int     `json:"days"`
}

type serverPlayer struct {
	Name     string `json:"name"`
	Account  string `json:"accountName"`
	PlayerID string `json:"playerId"`
	UserID   string `json:"userId"`
}

type serverPlayers struct {
	Players []serverPlayer `json:"players"`
}

type restClient struct {
	baseURL  *url.URL
	username string
	password func() (string, error)
	client   *http.Client
}

func newRESTClient(rawURL, username string, password func() (string, error)) (*restClient, error) {
	if username == "" {
		username = "admin"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse Palworld REST URL: %w", err)
	}
	if parsed.Scheme != "http" {
		return nil, errors.New("Palworld REST URL must use http on the local machine")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, errors.New("Palworld REST URL must use a loopback address")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return &restClient{
		baseURL:  parsed,
		username: username,
		password: password,
		client: &http.Client{
			Timeout: 4 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *restClient) info(ctx context.Context) (serverInfo, error) {
	var result serverInfo
	err := c.do(ctx, http.MethodGet, "/v1/api/info", nil, &result)
	return result, err
}

func (c *restClient) metrics(ctx context.Context) (serverMetrics, error) {
	var result serverMetrics
	err := c.do(ctx, http.MethodGet, "/v1/api/metrics", nil, &result)
	return result, err
}

func (c *restClient) players(ctx context.Context) (serverPlayers, error) {
	var result serverPlayers
	err := c.do(ctx, http.MethodGet, "/v1/api/players", nil, &result)
	return result, err
}

func (c *restClient) save(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/v1/api/save", struct{}{}, nil)
}

func (c *restClient) shutdown(ctx context.Context, waitSeconds int, message string) error {
	body := struct {
		WaitTime int    `json:"waittime"`
		Message  string `json:"message"`
	}{WaitTime: waitSeconds, Message: message}
	return c.do(ctx, http.MethodPost, "/v1/api/shutdown", body, nil)
}

func (c *restClient) do(ctx context.Context, method, path string, body, target any) error {
	password, err := c.password()
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("Palworld AdminPassword is empty; safe management is disabled")
	}

	var reader io.Reader
	if body != nil {
		data, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return marshalErr
		}
		reader = bytes.NewReader(data)
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return err
	}
	request.SetBasicAuth(c.username, password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("Palworld REST API unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusUnauthorized {
			return errors.New("Palworld REST API rejected AdminPassword")
		}
		return fmt.Errorf("Palworld REST API returned %s", response.Status)
	}
	if target == nil || response.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode Palworld REST API response: %w", err)
	}
	return nil
}
