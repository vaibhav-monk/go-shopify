package goshopify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

const shopifyChecksumHeader = "X-Shopify-Hmac-Sha256"

var accessTokenRelPath = "admin/oauth/access_token"

// Returns a Shopify oauth authorization url for the given shopname and state.
//
// State is a unique value that can be used to check the authenticity during a
// callback from Shopify.
func (app App) AuthorizeUrl(shopName string, state string) string {
	shopUrl, _ := url.Parse(ShopBaseUrl(shopName))
	shopUrl.Path = "/admin/oauth/authorize"
	query := shopUrl.Query()
	query.Set("client_id", app.ApiKey)
	query.Set("redirect_uri", app.RedirectUrl)
	query.Set("scope", app.Scope)
	query.Set("state", state)
	shopUrl.RawQuery = query.Encode()
	return shopUrl.String()
}

func (app App) GetAccessToken(shopName string, code string) (string, error) {
	type Token struct {
		Token string `json:"access_token"`
	}

	data := struct {
		ClientId     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Code         string `json:"code"`
	}{
		ClientId:     app.ApiKey,
		ClientSecret: app.ApiSecret,
		Code:         code,
	}

	client := app.Client
	if client == nil {
		client = NewClient(app, shopName, "")
	}

	req, err := client.NewRequest("POST", accessTokenRelPath, data, nil)
	if err != nil {
		return "", err
	}

	token := new(Token)
	err = client.Do(req, token)
	return token.Token, err
}

// Verify a message against a message HMAC
func (app App) VerifyMessage(message, messageMAC string) bool {
	mac := hmac.New(sha256.New, []byte(app.ApiSecret))
	mac.Write([]byte(message))
	expectedMAC := mac.Sum(nil)

	// shopify HMAC is in hex so it needs to be decoded
	actualMac, _ := hex.DecodeString(messageMAC)

	return hmac.Equal(actualMac, expectedMAC)
}

// Verifying URL callback parameters.
func (app App) VerifyAuthorizationURL(u *url.URL) (bool, error) {
	q := u.Query()
	messageMAC := q.Get("hmac")

	// Remove hmac and signature and leave the rest of the parameters alone.
	q.Del("hmac")
	q.Del("signature")

	message, err := url.QueryUnescape(q.Encode())

	return app.VerifyMessage(message, messageMAC), err
}

// Verifies a webhook http request, sent by Shopify.
// The body of the request is still readable after invoking the method.
func (app App) VerifyWebhookRequest(httpRequest *http.Request) bool {
	shopifySha256 := httpRequest.Header.Get(shopifyChecksumHeader)
	actualMac := []byte(shopifySha256)

	mac := hmac.New(sha256.New, []byte(app.ApiSecret))
	requestBody, _ := ioutil.ReadAll(httpRequest.Body)
	httpRequest.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))
	mac.Write(requestBody)
	macSum := mac.Sum(nil)
	expectedMac := []byte(base64.StdEncoding.EncodeToString(macSum))

	return hmac.Equal(actualMac, expectedMac)
}

// Verifies a webhook http request, sent by Shopify.
// The body of the request is still readable after invoking the method.
// This method has more verbose error output which is useful for debugging.
func (app App) VerifyWebhookRequestVerbose(httpRequest *http.Request) (bool, error) {
	if app.ApiSecret == "" {
		return false, errors.New("ApiSecret is empty")
	}

	shopifySha256 := httpRequest.Header.Get(shopifyChecksumHeader)
	if shopifySha256 == "" {
		return false, fmt.Errorf("header %s not set", shopifyChecksumHeader)
	}

	decodedReceivedHMAC, err := base64.StdEncoding.DecodeString(shopifySha256)
	if err != nil {
		return false, err
	}
	if len(decodedReceivedHMAC) != 32 {
		return false, fmt.Errorf("received HMAC is not of length 32, it is of length %d", len(decodedReceivedHMAC))
	}

	mac := hmac.New(sha256.New, []byte(app.ApiSecret))
	requestBody, err := ioutil.ReadAll(httpRequest.Body)
	if err != nil {
		return false, err
	}

	httpRequest.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))
	if len(requestBody) == 0 {
		return false, errors.New("request body is empty")
	}

	// Sha256 write doesn't actually return an error
	mac.Write(requestBody)

	computedHMAC := mac.Sum(nil)
	HMACSame := hmac.Equal(decodedReceivedHMAC, computedHMAC)
	if !HMACSame {
		return HMACSame, fmt.Errorf("expected hash %x does not equal %x", computedHMAC, decodedReceivedHMAC)
	}

	return HMACSame, nil
}

// GetOfflineAccessToken ...
func (app App) GetOfflineAccessToken(shopName, sessionToken string) (string, error) {

	type Token struct {
		Token string `json:"access_token"`
	}

	var data = struct {
		ClientID           string `json:"client_id"`
		ClientSecret       string `json:"client_secret"`
		SubjectToken       string `json:"subject_token"`
		SubjecyTokenType   string `json:"subject_token_type"`
		GrantType          string `json:"grant_type"`
		RequestedTokenType string `json:"requested_token_type"`
	}{
		ClientID:           app.ApiKey,
		ClientSecret:       app.ApiSecret,
		SubjectToken:       sessionToken,
		SubjecyTokenType:   "urn:ietf:params:oauth:token-type:id_token",
		GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
		RequestedTokenType: "urn:shopify:params:oauth:token-type:offline-access-token",
	}

	client := app.Client
	if client == nil {
		client = NewClient(app, shopName, "")
	}

	req, err := client.NewRequest("POST", accessTokenRelPath, data, nil)
	if err != nil {
		return "", err
	}

	token := new(Token)
	err = client.Do(req, token)
	return token.Token, err

}

// GetNameAndEmailFromOnlineAccessToken ...
func (app App) GetNameAndEmailFromOnlineAccessToken(shopName, sessionToken string) (string, string, error) {

	type Token struct {
		AssociatedUser struct {
			ID            int64  `json:"id"`
			FirstName     string `json:"first_name"`
			LastName      string `json:"last_name"`
			Email         string `json:"email"`
			AccountOwner  bool   `json:"account_owner"`
			Locale        string `json:"locale"`
			Collaborator  bool   `json:"collaborator"`
			EmailVerified bool   `json:"email_verified"`
		} `json:"associated_user"`
	}

	var data = struct {
		ClientID           string `json:"client_id"`
		ClientSecret       string `json:"client_secret"`
		SubjectToken       string `json:"subject_token"`
		SubjecyTokenType   string `json:"subject_token_type"`
		GrantType          string `json:"grant_type"`
		RequestedTokenType string `json:"requested_token_type"`
	}{
		ClientID:           app.ApiKey,
		ClientSecret:       app.ApiSecret,
		SubjectToken:       sessionToken,
		SubjecyTokenType:   "urn:ietf:params:oauth:token-type:id_token",
		GrantType:          "urn:ietf:params:oauth:grant-type:token-exchange",
		RequestedTokenType: "urn:shopify:params:oauth:token-type:online-access-token",
	}

	client := app.Client
	if client == nil {
		client = NewClient(app, shopName, "")
	}

	req, err := client.NewRequest("POST", accessTokenRelPath, data, nil)
	if err != nil {
		return "", "", err
	}

	token := new(Token)
	err = client.Do(req, token)
	return token.AssociatedUser.FirstName, token.AssociatedUser.Email, err

}

// Uninstall ... Uninstall the app from the shop
func (app App) Uninstall(shopName, accessToken string) error {
	client := app.Client
	if client == nil {
		client = NewClient(app, shopName, accessToken)
	}

	req, err := client.NewRequest("DELETE", "admin/api_permissions/current.json", nil, nil)
	if err != nil {
		return err
	}

	err = client.Do(req, nil)
	if err != nil {
		return err
	}

	return nil
}
