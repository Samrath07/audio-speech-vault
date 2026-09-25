package mail

import (
	"crypto/sha256"
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(Message) error
}

type FileSender struct{ Directory string }

func (s FileSender) Send(message Message) error {
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte(message.To + time.Now().UTC().String()))
	name := fmt.Sprintf("%d-%x.eml", time.Now().UTC().UnixNano(), hash[:6])
	contents := fmt.Sprintf("To: %s\nSubject: %s\n\n%s\n", message.To, message.Subject, message.Body)
	return os.WriteFile(filepath.Join(s.Directory, name), []byte(contents), 0o600)
}

type SMTPSender struct {
	Host, Port, Username, Password, From string
}

func (s SMTPSender) Send(message Message) error {
	address := s.Host + ":" + s.Port
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}
	body := strings.Join([]string{
		"From: " + s.From,
		"To: " + message.To,
		"Subject: " + message.Subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		message.Body,
	}, "\r\n")
	return smtp.SendMail(address, auth, s.From, []string{message.To}, []byte(body))
}
