// Package services berisi integrasi eksternal (captcha, email).
package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/edumaster/backend-go/internal/config"
	"github.com/edumaster/backend-go/internal/httpx"
)

const TestBypassCaptcha = "test-bypass-recaptcha"

type Captcha struct{ Config *config.Config }

type recaptchaResponse struct {
	Success    bool     `json:"success"`
	Score      float64  `json:"score"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

func (c *Captcha) Verify(ctx context.Context, token, remoteIP, expectedAction string) error {
	if !c.Config.RecaptchaEnabled {
		return nil
	}
	if c.Config.Environment == "test" && token == TestBypassCaptcha {
		return nil
	}
	if strings.TrimSpace(token) == "" {
		return httpx.BadRequest("Verifikasi captcha wajib diselesaikan.", "CAPTCHA_REQUIRED")
	}
	if c.Config.RecaptchaSecretKey == "" {
		return httpx.NewError(http.StatusServiceUnavailable, "Konfigurasi captcha belum lengkap. Hubungi administrator.", "CAPTCHA_NOT_CONFIGURED", nil)
	}

	form := url.Values{}
	form.Set("secret", c.Config.RecaptchaSecretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.Config.RecaptchaVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return httpx.NewError(http.StatusServiceUnavailable, "Layanan verifikasi captcha sedang tidak tersedia. Coba lagi.", "CAPTCHA_UNAVAILABLE", nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpx.NewError(http.StatusServiceUnavailable, "Layanan verifikasi captcha sedang tidak tersedia. Coba lagi.", "CAPTCHA_UNAVAILABLE", nil)
	}

	var payload recaptchaResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return httpx.NewError(http.StatusServiceUnavailable, "Layanan verifikasi captcha sedang tidak tersedia. Coba lagi.", "CAPTCHA_UNAVAILABLE", nil)
	}
	if !payload.Success {
		details := any(nil)
		if len(payload.ErrorCodes) > 0 {
			details = payload.ErrorCodes
		}
		return httpx.NewError(http.StatusBadRequest, "Verifikasi captcha gagal. Silakan ulangi.", "CAPTCHA_FAILED", details)
	}
	if payload.Score > 0 && payload.Score < c.Config.RecaptchaMinScore {
		return httpx.BadRequest("Aktivitas mencurigakan terdeteksi. Silakan coba lagi.", "CAPTCHA_LOW_SCORE")
	}
	if expectedAction != "" && payload.Action != "" && payload.Action != expectedAction {
		return httpx.BadRequest("Verifikasi captcha tidak cocok. Muat ulang halaman dan coba lagi.", "CAPTCHA_ACTION_MISMATCH")
	}
	return nil
}
