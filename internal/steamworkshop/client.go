package steamworkshop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"hearth/internal/mods"
	"hearth/internal/panel"
)

const (
	defaultDetailsEndpoint  = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"
	maxDetailsResponseBytes = 2 << 20
	maxWorkshopTitleRunes   = 256
)

type Client struct {
	httpClient *http.Client
	endpoint   string
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		endpoint:   defaultDetailsEndpoint,
	}
}

func newClient(httpClient *http.Client, endpoint string) *Client {
	return &Client{httpClient: httpClient, endpoint: endpoint}
}

func ParseReference(reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if validPublishedFileID(reference) {
		return reference, nil
	}
	parsed, err := url.Parse(reference)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		!strings.EqualFold(parsed.Hostname(), "steamcommunity.com") {
		return "", fmt.Errorf("%w: 请输入有效的 Steam Workshop ID 或链接", panel.ErrInvalid)
	}
	if parsed.Path != "/sharedfiles/filedetails/" && parsed.Path != "/sharedfiles/filedetails" &&
		parsed.Path != "/workshop/filedetails/" && parsed.Path != "/workshop/filedetails" {
		return "", fmt.Errorf("%w: 请输入有效的 Steam Workshop 详情链接", panel.ErrInvalid)
	}
	id := parsed.Query().Get("id")
	if !validPublishedFileID(id) {
		return "", fmt.Errorf("%w: Workshop 链接缺少有效 ID", panel.ErrInvalid)
	}
	return id, nil
}

func validPublishedFileID(value string) bool {
	if len(value) == 0 || len(value) > 20 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != "0"
}

func (c *Client) Lookup(ctx context.Context, appID, reference string) (mods.WorkshopItem, error) {
	id, err := ParseReference(reference)
	if err != nil {
		return mods.WorkshopItem{}, err
	}
	form := url.Values{"itemcount": {"1"}, "publishedfileids[0]": {id}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return mods.WorkshopItem{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return mods.WorkshopItem{}, fmt.Errorf("查询 Steam Workshop 失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return mods.WorkshopItem{}, fmt.Errorf("查询 Steam Workshop 失败: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDetailsResponseBytes+1))
	if err != nil {
		return mods.WorkshopItem{}, fmt.Errorf("读取 Steam Workshop 响应失败: %w", err)
	}
	if len(data) > maxDetailsResponseBytes {
		return mods.WorkshopItem{}, errors.New("Steam Workshop 返回的数据超过安全限制")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document detailsResponse
	if err := decoder.Decode(&document); err != nil {
		return mods.WorkshopItem{}, errors.New("Steam Workshop 返回了无法解析的数据")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return mods.WorkshopItem{}, errors.New("Steam Workshop 返回了额外数据")
	}
	if len(document.Response.Details) != 1 {
		return mods.WorkshopItem{}, fmt.Errorf("%w: Steam Workshop 中未找到该模组", panel.ErrNotFound)
	}
	detail := document.Response.Details[0]
	consumerAppID := detail.ConsumerAppID.String()
	if detail.Result != 1 || detail.PublishedFileID != id {
		return mods.WorkshopItem{}, fmt.Errorf("%w: Steam Workshop 中未找到该模组", panel.ErrNotFound)
	}
	if consumerAppID != appID {
		return mods.WorkshopItem{}, fmt.Errorf("%w: 该 Workshop 项目不属于 Palworld", panel.ErrInvalid)
	}
	if !safeTitle(detail.Title) {
		return mods.WorkshopItem{}, errors.New("Steam Workshop 返回的模组名称无效")
	}
	fileSize, err := strconv.ParseInt(detail.FileSize, 10, 64)
	if err != nil || fileSize < 0 {
		fileSize = 0
	}
	var updatedAt *time.Time
	if detail.TimeUpdated > 0 {
		value := time.Unix(detail.TimeUpdated, 0).UTC()
		updatedAt = &value
	}
	return mods.WorkshopItem{
		ID: id, AppID: appID, Title: strings.TrimSpace(detail.Title), CreatorID: detail.Creator,
		FileSize: fileSize, UpdatedAt: updatedAt,
		WorkshopURL: "https://steamcommunity.com/sharedfiles/filedetails/?id=" + id,
	}, nil
}

type detailsResponse struct {
	Response struct {
		Details []struct {
			PublishedFileID string      `json:"publishedfileid"`
			Result          int         `json:"result"`
			Creator         string      `json:"creator"`
			Title           string      `json:"title"`
			FileSize        string      `json:"file_size"`
			TimeUpdated     int64       `json:"time_updated"`
			ConsumerAppID   json.Number `json:"consumer_app_id"`
		} `json:"publishedfiledetails"`
	} `json:"response"`
}

func safeTitle(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	count := 0
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
		count++
		if count > maxWorkshopTitleRunes {
			return false
		}
	}
	return true
}
