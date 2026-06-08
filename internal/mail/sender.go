package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/config"
)

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

type SMTPConfig struct {
	Host        string
	Port        int
	SSL         bool
	User        string
	Password    string
	FromAddress string
	FromName    string
}

type SMTPSender struct {
	cfg SMTPConfig
}

func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port <= 0 {
		return nil, fmt.Errorf("SMTP host or port is empty")
	}
	if strings.TrimSpace(cfg.User) == "" || strings.TrimSpace(cfg.Password) == "" {
		return nil, fmt.Errorf("SMTP credentials are empty")
	}
	if strings.TrimSpace(cfg.FromAddress) == "" {
		return nil, fmt.Errorf("mail from address is empty")
	}
	if _, err := mail.ParseAddress(cfg.FromAddress); err != nil {
		return nil, fmt.Errorf("bad mail from address: %w", err)
	}
	if cfg.FromName == "" {
		cfg.FromName = "LwyLink"
	}
	return &SMTPSender{cfg: cfg}, nil
}

func NewSMTPSenderFromConfig(cfg config.MailConfig) (*SMTPSender, error) {
	return NewSMTPSender(SMTPConfig{
		Host:        cfg.SMTPHost,
		Port:        cfg.SMTPPort,
		SSL:         cfg.SMTPSSL,
		User:        cfg.SMTPUser,
		Password:    cfg.SMTPPassword,
		FromAddress: cfg.FromAddress,
		FromName:    cfg.FromName,
	})
}

func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	if _, err := mail.ParseAddress(to); err != nil {
		return fmt.Errorf("bad recipient address: %w", err)
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	conn, err := s.dial(ctx, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Quit()
	if err := client.Hello("lwylink.xyz"); err != nil {
		return err
	}
	if !s.cfg.SSL {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if err := client.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)); err != nil {
		return err
	}
	if err := client.Mail(s.cfg.FromAddress); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMessage(s.cfg, to, subject, body)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (s *SMTPSender) dial(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}
	if s.cfg.SSL {
		return tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12})
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func buildMessage(cfg SMTPConfig, to, subject, body string) []byte {
	from := mail.Address{Name: cfg.FromName, Address: cfg.FromAddress}
	var msg bytes.Buffer
	writeHeader(&msg, "From", from.String())
	writeHeader(&msg, "To", to)
	writeHeader(&msg, "Subject", mime.QEncoding.Encode("UTF-8", subject))
	writeHeader(&msg, "MIME-Version", "1.0")
	writeHeader(&msg, "Content-Type", `text/plain; charset="UTF-8"`)
	writeHeader(&msg, "Content-Transfer-Encoding", "base64")
	msg.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	for len(encoded) > 76 {
		msg.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	msg.WriteString(encoded + "\r\n")
	return msg.Bytes()
}

func writeHeader(buf *bytes.Buffer, key, value string) {
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", ""))
	buf.WriteString("\r\n")
}
