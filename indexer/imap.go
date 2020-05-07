/*
 * Meilindex - mail indexing and search tool.
 * Copyright (C) 2020 Tero Vierimaa
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 *
 */

package indexer

import (
	"crypto/tls"
	"fmt"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"github.com/jaytaylor/html2text"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
)

type Imap struct {
	Url                 string
	Tls                 bool
	TlsSkipVerification bool
	Username            string
	Password            string

	client  *client.Client
	mailbox *imap.MailboxStatus
}

func (i *Imap) Connect() error {
	var err error
	if i.Tls {
		if i.TlsSkipVerification {
			i.client, err = client.DialTLS(i.Url, &tls.Config{
				InsecureSkipVerify: true,
			})
		} else {
			i.client, err = client.DialTLS(i.Url, nil)
		}
	}

	if err != nil {
		err = fmt.Errorf("connect server: %v", err)
	} else {
		err = i.client.Login(i.Username, i.Password)
		if err != nil {
			err = fmt.Errorf("login: %v", err)
		}
	}

	return err
}

func (i *Imap) Disconnect() error {
	if i.client != nil {
		return i.client.Logout()
	}
	return nil
}

func (i *Imap) SelectMailbox(name string) error {
	mbox, err := i.client.Select(name, true)
	if err != nil {
		return err
	}
	i.mailbox = mbox

	logrus.Infof("Mailbox has %d mails", i.mailbox.Messages)

	return nil
}

func (i *Imap) FetchMail() ([]*Mail, error) {
	messages := make(chan *imap.Message, i.mailbox.Messages)
	done := make(chan error, 1)

	sequence := &imap.SeqSet{}
	start := 1
	stop := i.mailbox.Messages

	//stop = 10

	sequence.AddRange(stop, uint32(start))
	section := &imap.BodySectionName{}

	go func() {
		done <- i.client.Fetch(sequence, []imap.FetchItem{section.FetchItem(), imap.FetchUid}, messages)
	}()
	<-done

	mails := make([]*Mail, len(messages))

	folder := i.mailbox.Name
	if folder == "INBOX" {
		folder = "Inbox"
	}

	for i := 0; i < len(mails); i++ {
		msg := <-messages
		parsed, err := mail.CreateReader(msg.GetBody(section))
		if err != nil {
			logrus.Errorf("parse mail: %v", err)
		}

		m, err := mailToMail(parsed)
		m.Folder = folder

		mails[i] = m
	}

	return mails, nil
}

func mailToMail(m *mail.Reader) (*Mail, error) {
	var err error
	h := m.Header
	out := &Mail{
		From:    h.Get("From"),
		To:      h.Get("To"),
		Cc:      h.Get("Cc"),
		Date:    h.Get("Date"),
		Subject: h.Get("Subject"),
	}

	out.Id, err = h.MessageID()
	d, err := h.Date()
	if err == nil {
		out.Date = d.String()
	}

	s, err := h.Subject()
	if err == nil {
		out.Subject = s
	}

	for {
		part, err := m.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			logrus.Errorf("parse mail part: %v", err)
			break
		}

		switch part.Header.(type) {
		case *mail.InlineHeader:
			out.Body, err = html2text.FromReader(part.Body, html2text.Options{
				PrettyTables: false,
			})
		case *mail.AttachmentHeader:
			b, err := ioutil.ReadAll(part.Body)
			if err != nil {
				logrus.Errorf("read message attachment: %v", err)
			} else {
				out.Attachments = append(out.Attachments, b)
			}

		}
	}
	return out, err
}