package services

import (
	"crypto/tls"
	"fmt"
	"html"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/edumaster/backend-go/internal/config"
	"github.com/edumaster/backend-go/internal/httpx"
)

type Mail struct{ Config *config.Config }

func (m *Mail) BuildVerificationURL(token string) string {
	return m.Config.FrontendURL + "/verify-email?token=" + token
}

func (m *Mail) SendVerificationEmail(to, name, token string) (bool, error) {
	verifyURL := m.BuildVerificationURL(token)
	subject := "Verifikasi email akun EduMaster Anda"
	text := strings.Join([]string{
		"Halo " + name + ",",
		"",
		"Terima kasih sudah mendaftar di EduMaster.",
		fmt.Sprintf("Klik tautan berikut untuk memverifikasi email Anda (berlaku %d jam):", int(m.Config.EmailVerificationTTL.Hours())),
		verifyURL,
		"",
		"Jika Anda tidak merasa membuat akun, abaikan email ini.",
		"",
		"Salam,",
		"Tim EduMaster",
	}, "\n")
	htmlBody := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;color:#0f172a">
<h2>Verifikasi email Anda</h2>
<p>Halo <strong>%s</strong>,</p>
<p>Terima kasih sudah mendaftar di EduMaster. Klik tombol di bawah untuk mengaktifkan akun Anda.</p>
<p><a href="%s" style="display:inline-block;padding:12px 20px;border-radius:10px;background:#2563eb;color:#fff;text-decoration:none">Verifikasi Email</a></p>
<p>Atau salin tautan berikut: <a href="%s">%s</a></p>
<p>Jika Anda tidak merasa membuat akun, abaikan email ini.</p></div>`,
		html.EscapeString(name), html.EscapeString(verifyURL), html.EscapeString(verifyURL), html.EscapeString(verifyURL))

	return m.Send(to, subject, text, htmlBody)
}

func (m *Mail) Send(to, subject, text, htmlBody string) (bool, error) {
	if !m.Config.MailEnabled {
		if !m.Config.IsProduction {
			fmt.Printf("[MAIL DISABLED] to=%s subject=%s\n", to, subject)
		}
		return false, nil
	}
	if m.Config.Environment == "test" {
		return false, nil
	}
	if m.Config.MailHost == "" || m.Config.MailUsername == "" || m.Config.MailPassword == "" {
		return false, httpx.NewError(503, "Konfigurasi email belum lengkap. Hubungi administrator.", "MAIL_NOT_CONFIGURED", nil)
	}

	from := m.Config.MailFromAddress
	headers := []string{
		"From: " + mime.QEncoding.Encode("utf-8", m.Config.MailFromName) + " <" + from + ">",
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}
	if m.Config.MailReplyTo != "" {
		headers = append(headers, "Reply-To: "+m.Config.MailReplyTo)
	}
	message := []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + htmlBody)

	addr := net.JoinHostPort(m.Config.MailHost, strconv.Itoa(m.Config.MailPort))
	auth := smtp.PlainAuth("", m.Config.MailUsername, m.Config.MailPassword, m.Config.MailHost)

	if m.Config.MailPort == 465 || m.Config.MailEncryption == "ssl" {
		tlsConfig := &tls.Config{ServerName: m.Config.MailHost, MinVersion: tls.VersionTLS12}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return false, err
		}
		defer conn.Close()
		client, err := smtp.NewClient(conn, m.Config.MailHost)
		if err != nil {
			return false, err
		}
		defer client.Close()
		if err := client.Auth(auth); err != nil {
			return false, err
		}
		if err := client.Mail(from); err != nil {
			return false, err
		}
		if err := client.Rcpt(to); err != nil {
			return false, err
		}
		writer, err := client.Data()
		if err != nil {
			return false, err
		}
		if _, err := writer.Write(message); err != nil {
			return false, err
		}
		if err := writer.Close(); err != nil {
			return false, err
		}
		return true, client.Quit()
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, m.Config.MailHost)
	if err != nil {
		return false, err
	}
	defer client.Close()
	if m.Config.MailEncryption == "tls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.Config.MailHost, MinVersion: tls.VersionTLS12}); err != nil {
				return false, err
			}
		}
	}
	if err := client.Auth(auth); err != nil {
		return false, err
	}
	if err := client.Mail(from); err != nil {
		return false, err
	}
	if err := client.Rcpt(to); err != nil {
		return false, err
	}
	writer, err := client.Data()
	if err != nil {
		return false, err
	}
	if _, err := writer.Write(message); err != nil {
		return false, err
	}
	if err := writer.Close(); err != nil {
		return false, err
	}
	return true, client.Quit()
}
