package wechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientLoginEndpoints(t *testing.T) {
	requests := make([]string, 0, 2)
	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Method+" "+req.URL.Path+"?"+req.URL.RawQuery)
		if got := req.Header.Get("iLink-App-ClientVersion"); got != "1" {
			t.Fatalf("iLink-App-ClientVersion = %q, want 1", got)
		}
		switch req.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			return jsonResponse(http.StatusOK, `{"qrcode":"ticket-1","qrcode_img_content":"https://img.test/1"}`), nil
		case "/ilink/bot/get_qrcode_status":
			return jsonResponse(http.StatusOK, `{"status":"confirmed","bot_token":"bot-token","ilink_bot_id":"wx-bot","baseurl":"https://wechat.test"}`), nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.String())
			return nil, nil
		}
	}))

	client := NewLoginClient("https://wechat.test")
	qr, err := client.GetQRCode(context.Background())
	if err != nil {
		t.Fatalf("GetQRCode() error = %v", err)
	}
	if qr.Ticket != "ticket-1" || qr.ImageURL == "" {
		t.Fatalf("unexpected qr response: %+v", qr)
	}
	status, err := client.GetQRCodeStatus(context.Background(), "ticket-1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus() error = %v", err)
	}
	if status.AccountID != "wx-bot" || status.BotToken != "bot-token" {
		t.Fatalf("unexpected qr status: %+v", status)
	}
}

func TestClientBotEndpointsAndMediaUpload(t *testing.T) {
	var getUpdatesBody map[string]any
	sendBodies := make([]string, 0)
	uploadContentType := ""
	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		switch req.URL.Path {
		case "/ilink/bot/getupdates":
			if got := req.Header.Get("AuthorizationType"); got != "ilink_bot_token" {
				t.Fatalf("AuthorizationType = %q", got)
			}
			if err := json.Unmarshal(bodyBytes, &getUpdatesBody); err != nil {
				t.Fatalf("Unmarshal(getupdates body) error = %v", err)
			}
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0,"get_updates_buf":"buf-2","msgs":[]}`), nil
		case "/ilink/bot/sendmessage":
			sendBodies = append(sendBodies, string(bodyBytes))
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0}`), nil
		case "/ilink/bot/getuploadurl":
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0,"upload_full_url":"https://wechat.test/c2c/upload"}`), nil
		case "/c2c/upload":
			uploadContentType = req.Header.Get("Content-Type")
			resp := jsonResponse(http.StatusOK, `{}`)
			resp.Header.Set("x-encrypted-param", "enc-param")
			return resp, nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.String())
			return nil, nil
		}
	}))

	client := NewClient(Config{
		AccountID: "wx-bot",
		BotToken:  "bot-token",
		BaseURL:   "https://wechat.test",
	})
	updates, err := client.GetUpdates(context.Background(), "buf-1")
	if err != nil {
		t.Fatalf("GetUpdates() error = %v", err)
	}
	if updates.Buf != "buf-2" {
		t.Fatalf("GetUpdates().Buf = %q", updates.Buf)
	}
	if getUpdatesBody["get_updates_buf"] != "buf-1" {
		t.Fatalf("get_updates_buf body = %v", getUpdatesBody["get_updates_buf"])
	}

	if err := client.SendText(context.Background(), "user-1", "hello", "ctx-1"); err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(filePath, []byte("test-file"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := client.UploadAndSendMedia(context.Background(), "user-1", SendAction{
		Type:         "file",
		Path:         "report.pdf",
		ResolvedPath: filePath,
		FileName:     "report.pdf",
	}, "ctx-2"); err != nil {
		t.Fatalf("UploadAndSendMedia() error = %v", err)
	}

	if uploadContentType != "application/octet-stream" {
		t.Fatalf("upload content-type = %q", uploadContentType)
	}
	if len(sendBodies) < 2 {
		t.Fatalf("expected sendmessage to be called twice, got %d", len(sendBodies))
	}
	if !strings.Contains(sendBodies[0], `"text":"hello"`) {
		t.Fatalf("text sendmessage body missing text: %s", sendBodies[0])
	}
	if !strings.Contains(sendBodies[1], `"file_item"`) || !strings.Contains(sendBodies[1], `"encrypt_query_param":"enc-param"`) {
		t.Fatalf("file sendmessage body unexpected: %s", sendBodies[1])
	}
}

func TestClientDownloadAttachmentSupportsHexAndBase64Keys(t *testing.T) {
	plain := []byte("secret attachment")
	key := []byte("1234567890abcdef")
	ciphertext, err := encryptAES128ECB(plain, key)
	if err != nil {
		t.Fatalf("encryptAES128ECB() error = %v", err)
	}

	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/c2c/download" {
			t.Fatalf("unexpected path: %s", req.URL.String())
		}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(bytes.NewReader(ciphertext)),
		}
		return resp, nil
	}))

	client := NewClient(Config{AccountID: "wx-bot", BotToken: "bot-token", BaseURL: "https://wechat.test"})
	got, err := client.DownloadAttachment(context.Background(), WeChatAttachment{
		downloadQuery: "query",
		aesKeyHex:     "31323334353637383930616263646566",
	})
	if err != nil {
		t.Fatalf("DownloadAttachment(hex) error = %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("hex attachment = %q, want %q", got, plain)
	}

	got, err = client.DownloadAttachment(context.Background(), WeChatAttachment{
		downloadQuery: "query",
		aesKeyBase64:  base64.StdEncoding.EncodeToString(key),
	})
	if err != nil {
		t.Fatalf("DownloadAttachment(base64) error = %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("base64 attachment = %q, want %q", got, plain)
	}
}

func TestClientTypingEndpoints(t *testing.T) {
	var getConfigBody map[string]any
	var sendTypingBody map[string]any

	useHTTPClientFactory(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		switch req.URL.Path {
		case "/ilink/bot/getconfig":
			if err := json.Unmarshal(bodyBytes, &getConfigBody); err != nil {
				t.Fatalf("Unmarshal(getconfig body) error = %v", err)
			}
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0,"typing_ticket":"ticket-typing"}`), nil
		case "/ilink/bot/sendtyping":
			if err := json.Unmarshal(bodyBytes, &sendTypingBody); err != nil {
				t.Fatalf("Unmarshal(sendtyping body) error = %v", err)
			}
			return jsonResponse(http.StatusOK, `{"ret":0,"errcode":0}`), nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.String())
			return nil, nil
		}
	}))

	client := NewClient(Config{
		AccountID: "wx-bot",
		BotToken:  "bot-token",
		BaseURL:   "https://wechat.test",
	})
	ticket, err := client.GetTypingTicket(context.Background(), "user-typing", "ctx-typing")
	if err != nil {
		t.Fatalf("GetTypingTicket() error = %v", err)
	}
	if ticket != "ticket-typing" {
		t.Fatalf("ticket = %q, want ticket-typing", ticket)
	}
	if getConfigBody["ilink_user_id"] != "user-typing" {
		t.Fatalf("getconfig ilink_user_id = %v", getConfigBody["ilink_user_id"])
	}

	if err := client.SendTyping(context.Background(), "user-typing", "ticket-typing", typingStatusActive); err != nil {
		t.Fatalf("SendTyping() error = %v", err)
	}
	if got := sendTypingBody["status"]; got != float64(typingStatusActive) {
		t.Fatalf("sendtyping status = %v, want %d", got, typingStatusActive)
	}
	if got := sendTypingBody["typing_ticket"]; got != "ticket-typing" {
		t.Fatalf("sendtyping ticket = %v, want ticket-typing", got)
	}
}
