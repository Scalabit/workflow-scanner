package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

type EmailProcessor struct {
	IMAPServer  string
	SMTPServer  string
	Username    string
	Password    string
	APIEndpoint string
}

func NewEmailProcessor() *EmailProcessor {
	return &EmailProcessor{
		IMAPServer:  os.Getenv("IMAP_SERVER"),
		SMTPServer:  os.Getenv("SMTP_SERVER"),
		Username:    os.Getenv("EMAIL_USERNAME"),
		Password:    os.Getenv("EMAIL_PASSWORD"),
		APIEndpoint: "http://localhost:8000/process-invoice",
	}
}

func (e *EmailProcessor) ConnectIMAP() (*client.Client, error) {
	c, err := client.DialTLS(e.IMAPServer, nil)
	if err != nil {
		return nil, err
	}

	if err := c.Login(e.Username, e.Password); err != nil {
		return nil, err
	}

	return c, nil
}

func (e *EmailProcessor) GetAttachments(msg *mail.Message) []map[string]interface{} {
	var attachments []map[string]interface{}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		return attachments
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(msg.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}

			disposition := p.Header.Get("Content-Disposition")
			if strings.Contains(disposition, "attachment") {
				filename := p.FileName()
				if strings.HasSuffix(filename, ".pdf") || strings.HasSuffix(filename, ".txt") {
					content, err := io.ReadAll(p)
					if err != nil {
						continue
					}

					attachments = append(attachments, map[string]interface{}{
						"filename": filename,
						"content":  content,
					})
				}
			}
		}
	}

	return attachments
}

func (e *EmailProcessor) ProcessInvoiceAttachment(attachment map[string]interface{}) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", attachment["filename"].(string))
	if err != nil {
		return "", err
	}

	content := attachment["content"].([]byte)
	part.Write(content)
	writer.Close()

	req, err := http.NewRequest("POST", e.APIEndpoint, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(responseBody), nil
}

func (e *EmailProcessor) SendInvoiceReport(fromEmail, originalSubject, extractedData, attachmentName string) error {
	auth := smtp.PlainAuth("", e.Username, e.Password, strings.Split(e.SMTPServer, ":")[0])

	// Send to our own inbox (not sender's)
	to := []string{e.Username}
	
	body := fmt.Sprintf(`Invoice Processing Report

EXTRACTED DATA:
%s

ORIGINAL EMAIL DETAILS:
- From: %s
- Subject: %s
- Attachment: %s
- Processed: %s

---
Automated Invoice Processing System`, 
		extractedData, fromEmail, originalSubject, attachmentName, time.Now().Format("2006-01-02 15:04:05"))

	msg := fmt.Sprintf("To: %s\r\nSubject: Invoice Processed: %s\r\n\r\n%s", e.Username, originalSubject, body)

	return smtp.SendMail(e.SMTPServer, auth, e.Username, to, []byte(msg))
}

func (e *EmailProcessor) ProcessEmails() error {
	c, err := e.ConnectIMAP()
	if err != nil {
		return err
	}
	defer c.Logout()

	_, err = c.Select("INBOX", false)
	if err != nil {
		return err
	}

	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{"\\Seen"}
	uids, err := c.Search(criteria)
	if err != nil {
		return err
	}

	if len(uids) == 0 {
		return nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)

	section := &imap.BodySectionName{}
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, section.FetchItem()}, messages)
	}()

	for msg := range messages {
		if msg.Envelope == nil {
			continue
		}

		body := msg.GetBody(section)
		if body == nil {
			continue
		}

		mailMsg, err := mail.ReadMessage(body)
		if err != nil {
			continue
		}

		attachments := e.GetAttachments(mailMsg)
		for _, attachment := range attachments {
			extractedData, err := e.ProcessInvoiceAttachment(attachment)
			if err != nil {
				log.Printf("Error processing attachment: %v", err)
				continue
			}

			fromAddr := ""
			if len(msg.Envelope.From) > 0 {
				fromAddr = msg.Envelope.From[0].Address()
			}

			attachmentName := attachment["filename"].(string)
			if err := e.SendInvoiceReport(fromAddr, msg.Envelope.Subject, extractedData, attachmentName); err != nil {
				log.Printf("Error sending invoice report: %v", err)
			}
		}
	}

	if err := <-done; err != nil {
		return err
	}

	return nil
}