package mail

import (
	"log"
	"net/smtp"
	"crypto/tls"
	"text/template"
	"encoding/base64"
	"bytes"
)

type Mail struct {
	Address  string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	From     string `yaml:"from"`
}

var calTemplate = string(`Subject: {{ .Subject }}
To: {{ .User -}} <{{ .Email -}}>
From: Julius <{{ .NoReply -}}>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="0000"

--0000
Content-Type: text/calendar; charset="UTF-8"; method=REQUEST
Content-Transfer-Encoding: 7bit

{{ .Content }}

--0000
Content-Type: application/ics; name="invite.ics"
Content-Disposition: attachment; filename="invite.ics"
Content-Transfer-Encoding: base64

{{ .Invite }}

--0000--
`)

type data struct {
	Subject string
	Email string
	NoReply string
	Content string
	Invite string
	User string
}

func (mailer Mail) Send(name string, email string, content string, subject string) {

	// hostname is used by PlainAuth to validate the TLS certificate.
	hostname := mailer.Address
	auth := smtp.PlainAuth("", mailer.Username, mailer.Password, hostname)

	tlsconfig := &tls.Config {
		InsecureSkipVerify: false,
		ServerName: hostname,
	}

	conn, err := tls.Dial("tcp", hostname+":465", tlsconfig)
	if err != nil {
		log.Panic(err)
	}

	c, err := smtp.NewClient(conn, hostname)
	if err != nil {
		log.Panic(err)
	}

	if err = c.Auth(auth); err != nil {
		log.Panic(err)
	}

	if err = c.Mail(mailer.From); err != nil {
		log.Panic(err)
	}

	if err = c.Rcpt(email); err != nil {
		log.Panic(err)
	}

	calTmp, err := template.New("ical").Parse(calTemplate)
	if err != nil { panic(err) }
	var msg bytes.Buffer

	d := data{
		Subject: subject,
		Email: email,
		User: name,
		NoReply: mailer.From,
		Content: content,
		Invite: base64.URLEncoding.EncodeToString([]byte(content)),
	}
	err = calTmp.Execute(&msg, d)
	if err != nil { panic(err) }
	// Data
	w, err := c.Data()
	if err != nil {
		log.Panic(err)
	}

	_, err = w.Write(msg.Bytes())
	if err != nil {
		log.Panic(err)
	}

	err = w.Close()
	if err != nil {
		log.Panic(err)
	}

	c.Quit()
}
