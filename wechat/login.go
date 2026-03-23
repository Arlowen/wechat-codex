package wechat

import (
	"fmt"
	"time"

	"github.com/skip2/go-qrcode"
)

func DisplayQRCode(url string) {
	fmt.Println("[info] 微信登录二维码链接:", url)
	q, err := qrcode.New(url, qrcode.Low)
	if err == nil {
		fmt.Println(q.ToSmallString(false))
	} else {
		fmt.Println("[warn] 无法渲染二维码 ASCII，请点击以上链接查看。")
	}
}

func LoginFlow(runtimeDir string, apiBaseURL string, botType string) error {
	store := NewAccountStore(runtimeDir)
	client := NewClient(apiBaseURL, "")

	startResp, err := client.StartLogin(botType)
	if err != nil {
		return fmt.Errorf("微信登录未返回二维码信息: %v", err)
	}

	qrcodeID, _ := startResp["qrcode"].(string)
	qrcodeURL, _ := startResp["qrcode_img_content"].(string)

	if qrcodeID == "" || qrcodeURL == "" {
		return fmt.Errorf("微信登录未返回二维码信息: %v", startResp)
	}

	fmt.Println("[info] 请使用微信扫描下面的二维码完成授权：")
	DisplayQRCode(qrcodeURL)

	startedAt := time.Now()
	scanNotified := false

	for time.Since(startedAt) < 8*time.Minute {
		statusResp, err := client.GetQRCodeStatus(qrcodeID)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		state, _ := statusResp["status"].(string)
		if state == "scaned" && !scanNotified {
			fmt.Println("[info] 已扫码，请在手机上确认授权。")
			scanNotified = true
		}

		if state == "confirmed" {
			token, _ := statusResp["bot_token"].(string)
			accountID, _ := statusResp["ilink_bot_id"].(string)
			userID, _ := statusResp["ilink_user_id"].(string)
			baseURL, _ := statusResp["baseurl"].(string)
			if baseURL == "" {
				baseURL = apiBaseURL
			}

			if token == "" {
				return fmt.Errorf("微信登录已确认，但未返回 token")
			}

			err = store.SaveAccount(Account{
				Token:     token,
				AccountID: accountID,
				UserID:    userID,
				BaseURL:   baseURL,
			})
			if err != nil {
				return fmt.Errorf("凭证保存失败: %v", err)
			}

			fmt.Println("[ok] 微信登录成功，凭证保存至", store.accountPath)
			if userID != "" {
				fmt.Printf("[ok] 当前微信账号 user_id: %s\n", userID)
			}
			return nil
		}

		if state == "expired" {
			return fmt.Errorf("二维码已过期，请重新执行登录")
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("登录超时，请重新执行登录")
}
