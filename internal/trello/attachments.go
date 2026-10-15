package trello

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// MaxAttachmentBytes bounds both upload memory and download disk use.
const MaxAttachmentBytes = 10 << 20

// Upload sends a bounded multipart file to a fixed, authorized card route.
func (c *Client) Upload(ctx context.Context, cardID, name, fileName, mimeType string, data []byte, out any) error {
	if len(data) > MaxAttachmentBytes {
		return fmt.Errorf("attachment exceeds 10 MiB limit")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if name != "" {
		if err := writer.WriteField("name", name); err != nil {
			return err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": fileName}))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err = part.Write(data); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return c.request(ctx, http.MethodPost, "/cards/"+cardID+"/attachments", nil, body.Bytes(), writer.FormDataContentType(), out)
}

// Download uses OAuth headers only. The URL is built from verified metadata, never a supplied URL.
// All redirects are refused by the shared client, including same-origin redirects.
func (c *Client) Download(ctx context.Context, cardID, attachmentID, fileName string, dst io.Writer) (int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	path := "/cards/" + cardID + "/attachments/" + attachmentID + "/download/" + url.PathEscape(fileName)
	escape := func(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }
	for attempt := 0; attempt <= 3; attempt++ {
		if err := c.acquire(ctx, false); err != nil {
			return 0, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
		if err != nil {
			return 0, "", fmt.Errorf("build attachment request")
		}
		req.Header.Set("Authorization", `OAuth oauth_consumer_key="`+escape(c.key)+`", oauth_token="`+escape(c.token)+`"`)
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, "", fmt.Errorf("attachment request: %s", c.Scrub(err.Error()))
		}
		c.observe(resp.Header, false)
		if resp.StatusCode == http.StatusTooManyRequests {
			_ = resp.Body.Close()
			delay, delayErr := retryDelay(resp.Header, attempt)
			c.cooldown(delay, false)
			if delayErr != nil {
				return 0, "", delayErr
			}
			if attempt < 3 {
				c.mu.Lock()
				c.retries++
				c.mu.Unlock()
				continue
			}
			return 0, "", fmt.Errorf("trello HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return 0, "", fmt.Errorf("trello HTTP %d", resp.StatusCode)
		}
		if resp.ContentLength > MaxAttachmentBytes {
			_ = resp.Body.Close()
			return 0, "", fmt.Errorf("attachment exceeds 10 MiB limit")
		}
		n, copyErr := io.Copy(dst, io.LimitReader(resp.Body, MaxAttachmentBytes))
		// Probe beyond the cap without ever writing the extra byte to disk.
		var extra [1]byte
		var extraN int
		if copyErr == nil {
			extraN, err = io.ReadFull(resp.Body, extra[:])
			if err != nil && err != io.EOF {
				copyErr = err
			}
		}
		closeErr := resp.Body.Close()
		if copyErr != nil {
			return 0, "", fmt.Errorf("save attachment: %s", c.Scrub(copyErr.Error()))
		}
		if closeErr != nil {
			return 0, "", fmt.Errorf("close attachment response")
		}
		if extraN > 0 {
			return 0, "", fmt.Errorf("attachment exceeds 10 MiB limit")
		}
		return n, resp.Header.Get("Content-Type"), nil
	}
	return 0, "", fmt.Errorf("trello retry budget exhausted")
}
