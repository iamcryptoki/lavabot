package main

import (
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/kennygrant/sanitize"
	"github.com/lavab/api/client"
	"github.com/lavab/api/routes"
	man "github.com/lavab/pgp-manifest-go"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

func initReceiver(username, password, keyPath string) {
	keyFile, err := os.Open(keyPath)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Unable to open the private key file")
		return
	}

	keyring, err := openpgp.ReadArmoredKeyRing(keyFile)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Unable to connect parse the key")
		return
	}

	api, err := client.New(*apiURL, 0)
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Unable to connect to the Lavaboom API")
		return
	}

	token, err := api.CreateToken(&routes.TokensCreateRequest{
		Type:     "auth",
		Username: username,
		Password: password,
	})
	if err != nil {
		log.WithField("error", err.Error()).Fatal("Unable to sign into Lavaboom's API")
		return
	}

	api.Headers["Authorization"] = "Bearer " + token.ID

	api.Subscribe(token.ID, func(ev *client.Event) {
		log.Printf("INCOMING EVENT %s", ev.Type)

		// Only handle receipts
		if ev.Type != "receipt" {
			return
		}

		log.Printf("GETTING EMAIL %s", ev.ID)

		email, err := api.GetEmail(ev.ID)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to get a received email")
			return
		}

		log.Printf("GOT THE EMAIL %s", email.Name)

		if email.Kind != "manifest" {
			log.Errorf("Not dealing with an email manifest in %s", email.ID)
		}

		// Read body
		input := strings.NewReader(email.Body)
		result, err := armor.Decode(input)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to decode email's body's armor")
			return
		}
		md, err := openpgp.ReadMessage(result.Body, keyring, nil, nil)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to decrypt an email")
			return
		}
		contents, err := ioutil.ReadAll(md.UnverifiedBody)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to read email's body")
			return
		}

		log.Printf("DECODED EMAIL BODY")

		// Read manifest
		input = strings.NewReader(email.Manifest)
		result, err = armor.Decode(input)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to decode email's manifest's armor")
			return
		}
		md, err = openpgp.ReadMessage(result.Body, keyring, nil, nil)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to decrypt an email's manifest")
			return
		}
		rawman, err := ioutil.ReadAll(md.UnverifiedBody)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to read email's manifest")
			return
		}
		manifest, err := man.Parse(rawman)
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to parse the manifest")
			return
		}

		log.Printf("DECODED MANIFEST BODY")

		// Sanitize contents if it has HTML
		body := string(contents)
		if manifest.ContentType != "text/plain" {
			log.Printf("SANITIZED CONTENTS")
			body = sanitize.HTML(string(contents))
		}

		// Stringify the to field
		to := []string{}
		for _, x := range manifest.To {
			to = append(to, x.String())
		}

		resp, err := api.CreateEmail(&routes.EmailsCreateRequest{
			Kind: "raw",
			To:   []string{*grooveAddress},
			Body: `---------- Forwarded message ----------
From: ` + manifest.From.String() + `
Date: ` + time.Now().Format(time.RFC1123) + `
Subject: ` + manifest.Subject + `
To: ` + strings.Join(to, ", ") + `

` + body,
			Subject: manifest.Subject,
		})
		if err != nil {
			log.WithField("error", err.Error()).Error("Unable to send an email")
			return
		}

		log.Printf("Forwarded email %v from %s with title %s", resp, manifest.From.String(), manifest.Subject)
	})
}
