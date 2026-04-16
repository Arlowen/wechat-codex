package wechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultWechatBaseURL = "https://ilinkai.weixin.qq.com"

type APIClient interface {
	GetUpdates(buf string, timeoutSec int) (map[string]interface{}, error)
	SendText(toUserID, contextToken, text string) (string, error)
	GetConfig(ilinkUserID, contextToken string) (map[string]interface{}, error)
	SendTyping(ilinkUserID, typingTicket string, status int) error
}

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
		HTTPClient: &http.Client{},
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

func responseIntValue(resp map[string]interface{}, key string) (int, bool) {
	raw, ok := resp[key]
	if !ok {
		return 0, false
	}
	switch value := raw.(type) {
	case float64:
		return int(value), true
	case int:
		return value, true
	case int64:
		return int(value), true
	case json.Number:
		v, err := value.Int64()
		if err != nil {
			return 0, false
		}
		return int(v), true
	default:
		return 0, false
	}
}

func responseMessage(resp map[string]interface{}) string {
	for _, key := range []string{"errmsg", "err_msg", "message", "msg"} {
		if value, ok := resp[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ensureAPIResponseOK(endpoint string, resp map[string]interface{}) error {
	if code, ok := responseIntValue(resp, "ret"); ok && code != 0 {
		if msg := responseMessage(resp); msg != "" {
			return fmt.Errorf("API %s returned ret=%d: %s", endpoint, code, msg)
		}
		return fmt.Errorf("API %s returned ret=%d", endpoint, code)
	}
	if code, ok := responseIntValue(resp, "errcode"); ok && code != 0 {
		if msg := responseMessage(resp); msg != "" {
			return fmt.Errorf("API %s returned errcode=%d: %s", endpoint, code, msg)
		}
		return fmt.Errorf("API %s returned errcode=%d", endpoint, code)
	}
	return nil
}

func (c *Client) requestJSON(method, endpoint string, query url.Values, payload map[string]interface{}, auth bool, timeoutSec int, extraHeaders map[string]string) (map[string]interface{}, error) {
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

	ctx := context.Background()
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
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
	resp, err := c.requestJSON(http.MethodGet, "ilink/bot/get_bot_qrcode", query, nil, false, 30, nil)
	if err != nil {
		return nil, err
	}
	if err := ensureAPIResponseOK("ilink/bot/get_bot_qrcode", resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetQRCodeStatus(qrcode string) (map[string]interface{}, error) {
	query := url.Values{}
	query.Set("qrcode", qrcode)
	extra := map[string]string{"iLink-App-ClientVersion": "1"}
	resp, err := c.requestJSON(http.MethodGet, "ilink/bot/get_qrcode_status", query, nil, false, 40, extra)
	if err != nil {
		return nil, err
	}
	if err := ensureAPIResponseOK("ilink/bot/get_qrcode_status", resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetUpdates(buf string, timeoutSec int) (map[string]interface{}, error) {
	payload := map[string]interface{}{"get_updates_buf": buf}
	if timeoutSec < 5 {
		timeoutSec = 5
	}
	return c.requestJSON(http.MethodPost, "ilink/bot/getupdates", nil, payload, true, timeoutSec, nil)
}

func (c *Client) SendText(toUserID, contextToken, text string) (string, error) {
	clientID := fmt.Sprintf("tg-codex-wechat-%d", time.Now().UnixNano())
	msg := map[string]interface{}{
		"from_user_id":  "",
		"to_user_id":    toUserID,
		"client_id":     clientID,
		"message_type":  2, // MESSAGE_TYPE_BOT
		"message_state": 2, // MESSAGE_STATE_FINISH
		"item_list": []map[string]interface{}{
			{
				"type":      1, // MESSAGE_ITEM_TYPE_TEXT
				"text_item": map[string]interface{}{"text": text},
			},
		},
	}
	if strings.TrimSpace(contextToken) != "" {
		msg["context_token"] = contextToken
	}
	payload := map[string]interface{}{"msg": msg}
	resp, err := c.requestJSON(http.MethodPost, "ilink/bot/sendmessage", nil, payload, true, 20, nil)
	if err != nil {
		return clientID, err
	}
	if err := ensureAPIResponseOK("ilink/bot/sendmessage", resp); err != nil {
		return clientID, err
	}
	return clientID, err
}

func (c *Client) GetConfig(ilinkUserID, contextToken string) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"ilink_user_id": ilinkUserID,
		"context_token": contextToken,
	}
	resp, err := c.requestJSON(http.MethodPost, "ilink/bot/getconfig", nil, payload, true, 15, nil)
	if err != nil {
		return nil, err
	}
	if err := ensureAPIResponseOK("ilink/bot/getconfig", resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) SendTyping(ilinkUserID, typingTicket string, status int) error {
	payload := map[string]interface{}{
		"ilink_user_id": ilinkUserID,
		"typing_ticket": typingTicket,
		"status":        status,
	}
	resp, err := c.requestJSON(http.MethodPost, "ilink/bot/sendtyping", nil, payload, true, 15, nil)
	if err != nil {
		return err
	}
	return ensureAPIResponseOK("ilink/bot/sendtyping", resp)
}
