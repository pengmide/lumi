package wechat

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	apiTimeout      = 15 * time.Second
	pollTimeout     = 35 * time.Second
	cdnTimeout      = 30 * time.Second
	maxMediaBytes   = 200 * 1024 * 1024
	typingTyping    = "TYPING"
	typingCancel    = "CANCEL"
	cdnBaseURL      = "https://novac2c.cdn.weixin.qq.com/c2c"
	wechatMsgText   = 1
	wechatMsgVoice  = 2
	wechatMsgImage  = 3
	wechatMsgFile   = 4
	uploadImageType = 1
	uploadFileType  = 3
)

type Client struct {
	baseURL   string
	botToken  string
	accountID string
	http      *http.Client
}

var httpClientFactory = func() *http.Client {
	return &http.Client{}
}

type QRCode struct {
	Ticket   string
	ImageURL string
}

type QRCodeStatus struct {
	Status    string
	AccountID string
	BotToken  string
	BaseURL   string
}

type UpdatesResponse struct {
	Buf      string
	Messages []rawMessage
}

type rawMessage struct {
	FromUserID   string    `json:"from_user_id"`
	ContextToken string    `json:"context_token"`
	MessageID    string    `json:"msg_id"`
	ItemList     []rawItem `json:"item_list"`
}

type rawItem struct {
	Type      int           `json:"type"`
	TextItem  *textItem     `json:"text_item,omitempty"`
	VoiceItem *textItem     `json:"voice_item,omitempty"`
	ImageItem *rawMediaItem `json:"image_item,omitempty"`
	FileItem  *rawMediaItem `json:"file_item,omitempty"`
}

type textItem struct {
	Text string `json:"text"`
}

type rawMediaItem struct {
	Media    *rawMedia `json:"media,omitempty"`
	AESKey   string    `json:"aeskey,omitempty"`
	FileName string    `json:"file_name,omitempty"`
	Len      string    `json:"len,omitempty"`
}

type rawMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
}

type uploadedMedia struct {
	EncryptQueryParam string
	AESKeyForMessage  string
	CiphertextSize    int64
	RawSize           int64
	FileName          string
}

func NewClient(cfg Config) *Client {
	return &Client{
		baseURL:   normalizeConfig(cfg).BaseURL,
		botToken:  cfg.BotToken,
		accountID: cfg.AccountID,
		http:      httpClientFactory(),
	}
}

func NewLoginClient(baseURL string) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = baseURL
	return &Client{
		baseURL: normalizeConfig(cfg).BaseURL,
		http:    httpClientFactory(),
	}
}

func (c *Client) GetQRCode(ctx context.Context) (QRCode, error) {
	var resp struct {
		Ticket   string `json:"qrcode"`
		ImageURL string `json:"qrcode_img_content"`
	}
	if err := c.doLoginGet(ctx, "/ilink/bot/get_bot_qrcode?bot_type=3", pollTimeout, &resp); err != nil {
		return QRCode{}, err
	}
	if resp.Ticket == "" || resp.ImageURL == "" {
		return QRCode{}, errors.New("invalid qrcode response")
	}
	return QRCode{Ticket: resp.Ticket, ImageURL: resp.ImageURL}, nil
}

func (c *Client) GetQRCodeStatus(ctx context.Context, ticket string) (QRCodeStatus, error) {
	var resp struct {
		Status    string `json:"status"`
		BotToken  string `json:"bot_token"`
		AccountID string `json:"ilink_bot_id"`
		BaseURL   string `json:"baseurl"`
	}
	query := "/ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(ticket)
	if err := c.doLoginGet(ctx, query, pollTimeout, &resp); err != nil {
		return QRCodeStatus{}, err
	}
	return QRCodeStatus{
		Status:    resp.Status,
		AccountID: resp.AccountID,
		BotToken:  resp.BotToken,
		BaseURL:   strings.TrimRight(resp.BaseURL, "/"),
	}, nil
}

func (c *Client) GetUpdates(ctx context.Context, buf string) (UpdatesResponse, error) {
	var resp struct {
		Ret      int          `json:"ret"`
		ErrCode  int          `json:"errcode"`
		ErrMsg   string       `json:"errmsg"`
		Message  string       `json:"message"`
		Buf      string       `json:"get_updates_buf"`
		Messages []rawMessage `json:"msgs"`
	}
	if err := c.doBotPost(ctx, "/ilink/bot/getupdates", pollTimeout, map[string]any{
		"get_updates_buf": buf,
		"base_info":       map[string]any{},
	}, &resp); err != nil {
		return UpdatesResponse{}, err
	}
	if err := checkBusinessError(resp.Ret, resp.ErrCode, resp.ErrMsg, resp.Message); err != nil {
		return UpdatesResponse{}, err
	}
	return UpdatesResponse{Buf: resp.Buf, Messages: resp.Messages}, nil
}

func (c *Client) SendText(ctx context.Context, toUserID, text, contextToken string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var resp struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Message string `json:"message"`
	}
	if err := c.doBotPost(ctx, "/ilink/bot/sendmessage", apiTimeout, map[string]any{
		"msg": map[string]any{
			"to_user_id":    toUserID,
			"client_id":     randomUUID(),
			"message_type":  2,
			"message_state": 2,
			"item_list": []any{
				map[string]any{
					"type": 1,
					"text_item": map[string]any{
						"text": text,
					},
				},
			},
			"context_token": contextToken,
		},
		"base_info": map[string]any{},
	}, &resp); err != nil {
		return err
	}
	return checkBusinessError(resp.Ret, resp.ErrCode, resp.ErrMsg, resp.Message)
}

func (c *Client) UploadAndSendMedia(ctx context.Context, toUserID string, action SendAction, contextToken string) error {
	info, err := os.Stat(action.ResolvedPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", action.Path)
	}
	if info.Size() > maxMediaBytes {
		return fmt.Errorf("file too large: %s", action.Path)
	}

	fileData, err := os.ReadFile(action.ResolvedPath)
	if err != nil {
		return err
	}

	aesKey := make([]byte, 16)
	if _, err := rand.Read(aesKey); err != nil {
		return err
	}
	fileKey, err := randomHex(16)
	if err != nil {
		return err
	}
	aesKeyHex := hex.EncodeToString(aesKey)

	uploadURL, uploadParam, err := c.getUploadURL(ctx, toUserID, action, fileData, aesKeyHex, fileKey)
	if err != nil {
		return err
	}

	ciphertext, err := encryptAES128ECB(fileData, aesKey)
	if err != nil {
		return err
	}
	encryptQueryParam, err := c.uploadCiphertext(ctx, uploadURL, uploadParam, fileKey, ciphertext)
	if err != nil {
		return err
	}

	media := uploadedMedia{
		EncryptQueryParam: encryptQueryParam,
		AESKeyForMessage:  base64.StdEncoding.EncodeToString([]byte(aesKeyHex)),
		CiphertextSize:    int64(len(ciphertext)),
		RawSize:           int64(len(fileData)),
		FileName:          action.FileName,
	}
	return c.sendMediaMessage(ctx, toUserID, action, media, contextToken)
}

func (c *Client) DownloadAttachment(ctx context.Context, attachment WeChatAttachment) ([]byte, error) {
	if attachment.downloadQuery == "" {
		return nil, errors.New("missing encrypt_query_param")
	}
	u := cdnBaseURL + "/download?encrypted_query_param=" + url.QueryEscape(attachment.downloadQuery)
	req, err := http.NewRequestWithContext(withTimeoutContext(ctx, cdnTimeout), http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cdn download http %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("cdn returned empty data")
	}
	if len(raw) > maxMediaBytes {
		return nil, fmt.Errorf("file too large: %d", len(raw))
	}

	key, err := attachmentAESKey(attachment)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return raw, nil
	}
	return decryptAES128ECB(raw, key)
}

func (c *Client) GetTypingTicket(ctx context.Context, toUserID, contextToken string) (string, error) {
	var resp struct {
		Ret          int    `json:"ret"`
		ErrCode      int    `json:"errcode"`
		ErrMsg       string `json:"errmsg"`
		Message      string `json:"message"`
		TypingTicket string `json:"typing_ticket"`
	}
	if err := c.doBotPost(ctx, "/ilink/bot/getconfig", apiTimeout, map[string]any{
		"ilink_user_id": toUserID,
		"context_token": contextToken,
		"base_info":     map[string]any{},
	}, &resp); err != nil {
		return "", err
	}
	if err := checkBusinessError(resp.Ret, resp.ErrCode, resp.ErrMsg, resp.Message); err != nil {
		return "", err
	}
	if resp.TypingTicket == "" {
		return "", errors.New("typing_ticket missing")
	}
	return resp.TypingTicket, nil
}

func (c *Client) SendTyping(ctx context.Context, toUserID, typingTicket string, status int) error {
	var resp struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Message string `json:"message"`
	}
	if err := c.doBotPost(ctx, "/ilink/bot/sendtyping", apiTimeout, map[string]any{
		"ilink_user_id": toUserID,
		"typing_ticket": typingTicket,
		"status":        status,
		"base_info":     map[string]any{},
	}, &resp); err != nil {
		return err
	}
	return checkBusinessError(resp.Ret, resp.ErrCode, resp.ErrMsg, resp.Message)
}

func (c *Client) getUploadURL(ctx context.Context, toUserID string, action SendAction, fileData []byte, aesKeyHex, fileKey string) (string, string, error) {
	var resp struct {
		Ret         int    `json:"ret"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		Message     string `json:"message"`
		UploadURL   string `json:"upload_full_url"`
		UploadParam string `json:"upload_param"`
	}
	mediaType := uploadFileType
	if action.Type == "image" {
		mediaType = uploadImageType
	}

	sum := md5.Sum(fileData)
	if err := c.doBotPost(ctx, "/ilink/bot/getuploadurl", apiTimeout, map[string]any{
		"filekey":       fileKey,
		"media_type":    mediaType,
		"to_user_id":    toUserID,
		"rawsize":       len(fileData),
		"rawfilemd5":    hex.EncodeToString(sum[:]),
		"filesize":      paddedSize(len(fileData)),
		"no_need_thumb": true,
		"aeskey":        aesKeyHex,
		"base_info":     map[string]any{},
	}, &resp); err != nil {
		return "", "", err
	}
	if err := checkBusinessError(resp.Ret, resp.ErrCode, resp.ErrMsg, resp.Message); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(resp.UploadURL) == "" && strings.TrimSpace(resp.UploadParam) == "" {
		return "", "", errors.New("getuploadurl missing upload url")
	}
	return strings.TrimSpace(resp.UploadURL), strings.TrimSpace(resp.UploadParam), nil
}

func (c *Client) uploadCiphertext(ctx context.Context, uploadURL, uploadParam, fileKey string, ciphertext []byte) (string, error) {
	targetURL := strings.TrimSpace(uploadURL)
	if targetURL == "" {
		if strings.TrimSpace(uploadParam) == "" {
			return "", errors.New("cdn upload url missing")
		}
		targetURL = cdnBaseURL + "/upload?encrypted_query_param=" + url.QueryEscape(uploadParam) + "&filekey=" + url.QueryEscape(fileKey)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(withTimeoutContext(ctx, cdnTimeout), http.MethodPost, targetURL, bytes.NewReader(ciphertext))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
		} else {
			resp.Body.Close()
			errHeader := strings.TrimSpace(resp.Header.Get("x-error-message"))
			encryptQueryParam := strings.TrimSpace(resp.Header.Get("x-encrypted-param"))
			switch {
			case resp.StatusCode >= 400 && resp.StatusCode < 500:
				return "", fmt.Errorf("cdn upload client error %d: %s", resp.StatusCode, errHeader)
			case resp.StatusCode != http.StatusOK:
				lastErr = fmt.Errorf("cdn upload server error %d: %s", resp.StatusCode, errHeader)
			case encryptQueryParam == "":
				lastErr = errors.New("cdn upload response missing x-encrypted-param header")
			default:
				return encryptQueryParam, nil
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("cdn upload failed")
	}
	return "", lastErr
}

func (c *Client) sendMediaMessage(ctx context.Context, toUserID string, action SendAction, media uploadedMedia, contextToken string) error {
	var item any
	if action.Type == "image" {
		item = map[string]any{
			"type": wechatMsgImage,
			"image_item": map[string]any{
				"media": map[string]any{
					"encrypt_query_param": media.EncryptQueryParam,
					"aes_key":             media.AESKeyForMessage,
					"encrypt_type":        1,
				},
				"mid_size": media.CiphertextSize,
			},
		}
	} else {
		item = map[string]any{
			"type": wechatMsgFile,
			"file_item": map[string]any{
				"media": map[string]any{
					"encrypt_query_param": media.EncryptQueryParam,
					"aes_key":             media.AESKeyForMessage,
					"encrypt_type":        1,
				},
				"file_name": media.FileName,
				"len":       strconv.FormatInt(media.RawSize, 10),
			},
		}
	}

	var resp struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Message string `json:"message"`
	}
	if err := c.doBotPost(ctx, "/ilink/bot/sendmessage", apiTimeout, map[string]any{
		"msg": map[string]any{
			"to_user_id":    toUserID,
			"client_id":     randomUUID(),
			"message_type":  2,
			"message_state": 2,
			"item_list":     []any{item},
			"context_token": contextToken,
		},
		"base_info": map[string]any{},
	}, &resp); err != nil {
		return err
	}
	return checkBusinessError(resp.Ret, resp.ErrCode, resp.ErrMsg, resp.Message)
}

func (c *Client) doLoginGet(ctx context.Context, path string, timeout time.Duration, out any) error {
	u := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(withTimeoutContext(ctx, timeout), http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) doBotPost(ctx context.Context, path string, timeout time.Duration, payload any, out any) error {
	if strings.TrimSpace(c.botToken) == "" || strings.TrimSpace(c.accountID) == "" {
		return errors.New("wechat credentials are incomplete")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	u := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(withTimeoutContext(ctx, timeout), http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("X-WECHAT-UIN", c.accountID)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func checkBusinessError(ret, errCode int, errMsg, message string) error {
	if ret == 0 && errCode == 0 {
		return nil
	}
	text := strings.TrimSpace(errMsg)
	if text == "" {
		text = strings.TrimSpace(message)
	}
	if text == "" {
		text = "wechat api error"
	}
	return fmt.Errorf("%s (ret=%d errcode=%d)", text, ret, errCode)
}

func withTimeoutContext(ctx context.Context, timeout time.Duration) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	child, _ := context.WithTimeout(ctx, timeout)
	return child
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomUUID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func paddedSize(raw int) int {
	blockSize := aes.BlockSize
	padding := blockSize - (raw % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	return raw + padding
}

func encryptAES128ECB(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plain, block.BlockSize())
	out := make([]byte, len(padded))
	for start := 0; start < len(padded); start += block.BlockSize() {
		block.Encrypt(out[start:start+block.BlockSize()], padded[start:start+block.BlockSize()])
	}
	return out, nil
}

func decryptAES128ECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, errors.New("invalid aes-ecb ciphertext length")
	}
	out := make([]byte, len(ciphertext))
	for start := 0; start < len(ciphertext); start += block.BlockSize() {
		block.Decrypt(out[start:start+block.BlockSize()], ciphertext[start:start+block.BlockSize()])
	}
	return pkcs7Unpad(out, block.BlockSize())
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid pkcs7 data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errors.New("invalid pkcs7 padding")
	}
	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, errors.New("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padding], nil
}

func attachmentAESKey(attachment WeChatAttachment) ([]byte, error) {
	if attachment.aesKeyHex != "" {
		return hex.DecodeString(attachment.aesKeyHex)
	}
	if attachment.aesKeyBase64 == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(attachment.aesKeyBase64)
	if err != nil {
		return nil, err
	}
	switch len(decoded) {
	case 16:
		return decoded, nil
	case 32:
		return hex.DecodeString(string(decoded))
	default:
		return nil, fmt.Errorf("unexpected aes key length: %d", len(decoded))
	}
}

func detectAttachmentKind(data []byte, fileName string) string {
	contentType := http.DetectContentType(data)
	if strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != "" {
		if byExt := mime.TypeByExtension(ext); strings.HasPrefix(byExt, "image/") {
			return "image"
		}
	}
	return "file"
}
