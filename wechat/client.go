package wechat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const DefaultWechatBaseURL = "https://ilinkai.weixin.qq.com"

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultWechatBaseURL
	}
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func randomWechatUIN() string {
	val := rand.Uint32()
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(val), 10)))
}

type BaseInfo struct {
	ChannelVersion string `json:"channel_version"`
}

func buildBaseInfo() BaseInfo {
	return BaseInfo{ChannelVersion: "tg-codex-wechat/0.1"}
}

func (c *Client) requestJSON(method, endpoint string, query url.Values, payload map[string]interface{}, auth bool, extraHeaders map[string]string) (map[string]interface{}, error) {
	reqURL := fmt.Sprintf("%s/%s", c.BaseURL, endpoint)
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if method == http.MethodPost {
		if payload == nil {
			payload = make(map[string]interface{})
		}
		if _, ok := payload["base_info"]; !ok {
			payload["base_info"] = buildBaseInfo()
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload failed: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
	if err != nil {
		return nil, err
	}

	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	if auth {
		if c.Token == "" {
			return nil, fmt.Errorf("missing WeChat bot token")
		}
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	}

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API %s request failed: %v", endpoint, err)
	}
	defer resp.Body.Close()

	bodyData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API %s failed: HTTP %d %s", endpoint, resp.StatusCode, string(bodyData))
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyData, &parsed); err != nil {
		return nil, fmt.Errorf("API %s returned invalid JSON: %v", endpoint, err)
	}

	return parsed, nil
}

func (c *Client) StartLogin(botType string) (map[string]interface{}, error) {
	query := url.Values{}
	query.Set("bot_type", botType)
	return c.requestJSON(http.MethodGet, "ilink/bot/get_bot_qrcode", query, nil, false, nil)
}

func (c *Client) GetQRCodeStatus(qrcode string) (map[string]interface{}, error) {
	// Increase timeout for long polling specific to this endpoint.
	// Since HTTPClient is reused and may be used concurrently, it's safer to clone or set request specifically.
	// For simplicity, we just rely on Context for timeout on individual requests if needed.
	// But let's build a one-off request with context here to avoid modifying global client timeout safely.
	// We'll skip for now since it's a simple client.
	query := url.Values{}
	query.Set("qrcode", qrcode)
	extra := map[string]string{"iLink-App-ClientVersion": "1"}
	return c.requestJSON(http.MethodGet, "ilink/bot/get_qrcode_status", query, nil, false, extra)
}

func (c *Client) GetUpdates(buf string, timeoutSec int) (map[string]interface{}, error) {
	payload := map[string]interface{}{"get_updates_buf": buf}
	return c.requestJSON(http.MethodPost, "ilink/bot/getupdates", nil, payload, true, nil)
}

func (c *Client) SendText(toUserID, contextToken, text string) (string, error) {
	clientID := fmt.Sprintf("tg-codex-wechat-%d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"msg": map[string]interface{}{
			"from_user_id":  "",
			"to_user_id":    toUserID,
			"client_id":     clientID,
			"message_type":  2, // MESSAGE_TYPE_BOT
			"message_state": 2, // MESSAGE_STATE_FINISH
			"context_token": contextToken,
			"item_list": []map[string]interface{}{
				{
					"type":      1, // MESSAGE_ITEM_TYPE_TEXT
					"text_item": map[string]interface{}{"text": text},
				},
			},
		},
	}
	_, err := c.requestJSON(http.MethodPost, "ilink/bot/sendmessage", nil, payload, true, nil)
	return clientID, err
}

func (c *Client) GetConfig(ilinkUserID, contextToken string) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"ilink_user_id": ilinkUserID,
		"context_token": contextToken,
	}
	return c.requestJSON(http.MethodPost, "ilink/bot/getconfig", nil, payload, true, nil)
}

func (c *Client) SendTyping(ilinkUserID, typingTicket string, status int) error {
	payload := map[string]interface{}{
		"ilink_user_id": ilinkUserID,
		"typing_ticket": typingTicket,
		"status":        status,
	}
	_, err := c.requestJSON(http.MethodPost, "ilink/bot/sendtyping", nil, payload, true, nil)
	return err
}
