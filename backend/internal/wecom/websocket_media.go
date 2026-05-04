package wecom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const wecomWSMediaMaxBytes = maxMediaBytes

type wsMediaRef struct {
	URL    string
	Aeskey string
}

type wsMixedItem struct {
	MsgType string `json:"msgtype"`
	Text    *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Image *struct {
		URL    string `json:"url"`
		Aeskey string `json:"aeskey"`
	} `json:"image,omitempty"`
	File *struct {
		URL    string `json:"url"`
		Aeskey string `json:"aeskey"`
	} `json:"file,omitempty"`
}

type wsMixedBlock struct {
	MsgItem []wsMixedItem `json:"msg_item"`
}

type wsQuoteBlock struct {
	MsgType string `json:"msgtype"`
	Text    *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Voice *struct {
		Content string `json:"content"`
	} `json:"voice,omitempty"`
	Image *struct {
		URL    string `json:"url"`
		Aeskey string `json:"aeskey"`
	} `json:"image,omitempty"`
	File *struct {
		URL    string `json:"url"`
		Aeskey string `json:"aeskey"`
	} `json:"file,omitempty"`
	Mixed *wsMixedBlock `json:"mixed,omitempty"`
}

func wsCollectInboundParts(body *wsMsgCallbackBody) (texts []string, imgs, files []wsMediaRef) {
	appendText := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			texts = append(texts, s)
		}
	}
	appendImage := func(url, aeskey string) {
		if url != "" {
			imgs = append(imgs, wsMediaRef{URL: url, Aeskey: aeskey})
		}
	}
	appendFile := func(url, aeskey string) {
		if url != "" {
			files = append(files, wsMediaRef{URL: url, Aeskey: aeskey})
		}
	}
	walkMixed := func(m *wsMixedBlock) {
		if m == nil {
			return
		}
		for _, item := range m.MsgItem {
			switch item.MsgType {
			case "text":
				if item.Text != nil {
					appendText(item.Text.Content)
				}
			case "image":
				if item.Image != nil {
					appendImage(item.Image.URL, item.Image.Aeskey)
				}
			case "file":
				if item.File != nil {
					appendFile(item.File.URL, item.File.Aeskey)
				}
			}
		}
	}
	walkQuote := func(q *wsQuoteBlock) {
		if q == nil {
			return
		}
		switch q.MsgType {
		case "text":
			if q.Text != nil {
				appendText(q.Text.Content)
			}
		case "voice":
			if q.Voice != nil {
				appendText(q.Voice.Content)
			}
		case "image":
			if q.Image != nil {
				appendImage(q.Image.URL, q.Image.Aeskey)
			}
		case "file":
			if q.File != nil {
				appendFile(q.File.URL, q.File.Aeskey)
			}
		case "mixed":
			walkMixed(q.Mixed)
		}
	}

	if body.Mixed != nil && len(body.Mixed.MsgItem) > 0 {
		walkMixed(body.Mixed)
	} else {
		appendText(body.Text.Content)
		if body.Image != nil {
			appendImage(body.Image.URL, body.Image.Aeskey)
		}
		if body.MsgType == "file" && body.File != nil {
			appendFile(body.File.URL, body.File.Aeskey)
		}
	}
	if body.Mixed != nil && len(body.Mixed.MsgItem) > 0 {
		if body.MsgType == "file" && body.File != nil {
			appendFile(body.File.URL, body.File.Aeskey)
		}
		if body.MsgType == "image" && body.Image != nil {
			appendImage(body.Image.URL, body.Image.Aeskey)
		}
	}
	walkQuote(body.Quote)
	return texts, imgs, files
}

func decodeWeComAESKey(aesKey string) ([]byte, error) {
	s := strings.TrimSpace(aesKey)
	if s == "" {
		return nil, fmt.Errorf("wecom-ws: empty aeskey")
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n', '\r', ' ', '\t':
			continue
		default:
			b.WriteByte(s[i])
		}
	}
	s = b.String()

	if len(s) == 64 && isHexString(s) {
		key, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("wecom-ws: decode aeskey hex: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("wecom-ws: aeskey hex length %d, want 32 bytes", len(key))
		}
		return key, nil
	}

	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")

	switch len(s) % 4 {
	case 0:
	case 2:
		s += "=="
	case 3:
		s += "="
	default:
		return nil, fmt.Errorf("wecom-ws: invalid aeskey base64 length")
	}

	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("wecom-ws: decode aeskey: %w", err)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("wecom-ws: aeskey decoded length %d, need >= 32", len(key))
	}
	return key, nil
}

func isHexString(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func wecomDecryptFile(ciphertext []byte, aesKeyB64 string) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, fmt.Errorf("wecom-ws: empty ciphertext")
	}
	key, err := decodeWeComAESKey(aesKeyB64)
	if err != nil {
		return nil, err
	}
	key32 := key[:32]
	iv := key32[:16]

	block, err := aes.NewCipher(key32)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("wecom-ws: ciphertext not multiple of block size")
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	return pkcs7UnpadWeCom(plain)
}

func pkcs7UnpadWeCom(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("wecom-ws: empty padded data")
	}
	padLen := int(data[len(data)-1])
	if padLen < 1 || padLen > 32 || padLen > len(data) {
		return nil, fmt.Errorf("wecom-ws: invalid pkcs7 pad length %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if int(data[i]) != padLen {
			return nil, fmt.Errorf("wecom-ws: invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padLen], nil
}

func parseContentDispositionFilename(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	lower := strings.ToLower(h)
	if idx := strings.Index(lower, "filename*="); idx >= 0 {
		val := strings.TrimSpace(h[idx+len("filename*="):])
		val = strings.TrimSuffix(strings.TrimSpace(val), ";")
		if after, ok := strings.CutPrefix(val, "UTF-8''"); ok {
			if dec, err := url.QueryUnescape(after); err == nil {
				return filepath.Base(dec)
			}
			return filepath.Base(after)
		}
	}
	if idx := strings.Index(lower, "filename="); idx >= 0 {
		val := strings.TrimSpace(h[idx+len("filename="):])
		val = strings.TrimSuffix(val, ";")
		val = strings.Trim(val, `"`)
		if dec, err := url.QueryUnescape(val); err == nil {
			return filepath.Base(dec)
		}
		return filepath.Base(val)
	}
	return ""
}

func downloadWeComWSMedia(ctx context.Context, urlStr, aesKey string) (data []byte, fileName string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, "", err
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("wecom-ws: download HTTP %s", resp.Status)
	}
	fileName = parseContentDispositionFilename(resp.Header.Get("Content-Disposition"))
	lim := io.LimitReader(resp.Body, wecomWSMediaMaxBytes+1)
	raw, err := io.ReadAll(lim)
	if err != nil {
		return nil, "", err
	}
	if len(raw) > wecomWSMediaMaxBytes {
		return nil, "", fmt.Errorf("wecom-ws: media larger than %d bytes", wecomWSMediaMaxBytes)
	}
	if aesKey != "" {
		raw, err = wecomDecryptFile(raw, aesKey)
		if err != nil {
			return nil, "", err
		}
	}
	return raw, fileName, nil
}

func wecomInboundFileMime(fileName string, data []byte) string {
	if fileName != "" {
		if mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName))); mt != "" {
			return mt
		}
	}
	if len(data) == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(data)
}
