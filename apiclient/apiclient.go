// Copyright 2018 Paul Furley and Ian Drysdale
//
// This file is part of Fluidkeys Client which makes it simple to use OpenPGP.
//
// Fluidkeys Client is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Fluidkeys Client is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with Fluidkeys Client.  If not, see <https://www.gnu.org/licenses/>.

package apiclient

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fluidkeys/api/v1structs"
	"github.com/fluidkeys/crypto/openpgp/clearsign"
	fpr "github.com/fluidkeys/fluidkeys/fingerprint"
	"github.com/fluidkeys/fluidkeys/pgpkey"
	"github.com/fluidkeys/fluidkeys/team"
	"github.com/gofrs/uuid"
)

const (
	defaultBaseURL = "https://api.fluidkeys.com/v1/"
	userAgent      = "fluidkeys"
)

// A Client manages communication with the Fluidkeys Server API.
type Client struct {
	client    *http.Client // HTTP client used to communicate with the API.
	BaseURL   *url.URL     // Base URL for API requests
	UserAgent string       // User agent used when communicating with the  API.
}

var (
	// ErrPublicKeyNotFound means the response was OK, but no key was found
	ErrPublicKeyNotFound = fmt.Errorf("Public key not found")

	// ErrTeamNotFound means the response was OK, but no team was found
	ErrTeamNotFound = fmt.Errorf("Team not found")

	// ErrForbidden means the given user doesn't have access to the given resource, for example
	// the requester key isn't a member of a requested team.
	ErrForbidden = fmt.Errorf("Forbidden")
)

// New returns a new Fluidkeys Server API client.
func New(fluidkeysVersion string) *Client {
	apiURL, got := os.LookupEnv("FLUIDKEYS_API_URL") // e.g. http://localhost:4747/v1/
	if !got {
		apiURL = defaultBaseURL
	}

	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		log.Panic(fmt.Errorf("error parsing URL '%s': %v", apiURL, err))
	}

	return &Client{
		client:    http.DefaultClient,
		BaseURL:   parsedURL,
		UserAgent: userAgent + "-" + fluidkeysVersion,
	}
}

// GetPublicKey attempts to get a single armored public key.
func (c *Client) GetPublicKey(email string) (string, error) {
	path := fmt.Sprintf("email/%s/key", url.QueryEscape(email))
	request, err := c.newRequest("GET", path, nil)
	if err != nil {
		return "", err
	}
	decodedJSON := new(v1structs.GetPublicKeyResponse)
	response, err := c.do(request, &decodedJSON)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return "", ErrPublicKeyNotFound
		}
		return "", err
	}

	return decodedJSON.ArmoredPublicKey, nil
}

// GetPublicKeyByFingerprint attempts to get a single armored public key.
func (c *Client) GetPublicKeyByFingerprint(fingerprint fpr.Fingerprint) (*pgpkey.PgpKey, error) {
	path := fmt.Sprintf("key/%s.asc", fingerprint.Hex())
	request, err := c.newRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if !isSuccess(response.StatusCode) {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return nil, ErrPublicKeyNotFound
		}
		return nil, makeErrorForAPIResponse(response)
	}

	if response.Body == nil {
		return nil, fmt.Errorf("got http %d, but with missing body", response.StatusCode)
	}

	bodyData, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	if len(bodyData) == 0 {
		return nil, fmt.Errorf("got http %d, but with empty body", response.StatusCode)
	}

	retrievedKey, err := pgpkey.LoadFromArmoredPublicKey(string(bodyData))
	if err != nil {
		return nil, fmt.Errorf("failed to load armored key: %v", err)
	}

	if retrievedKey.Fingerprint() != fingerprint {
		log.Printf("danger: requested key %s from API but got back key %s\n",
			fingerprint, retrievedKey.Fingerprint())

		return nil, fmt.Errorf(
			"requested key %s but got back %s",
			fingerprint, retrievedKey.Fingerprint(),
		)
	}

	return retrievedKey, nil
}

// CreateSecret creates a secret for the given recipient
func (c *Client) CreateSecret(recipientFingerprint fpr.Fingerprint, armoredEncryptedSecret string) error {
	sendSecretRequest := v1structs.SendSecretRequest{
		RecipientFingerprint:   recipientFingerprint.Uri(),
		ArmoredEncryptedSecret: armoredEncryptedSecret,
	}
	request, err := c.newRequest("POST", "secrets", sendSecretRequest)
	if err != nil {
		return err
	}

	_, err = c.do(request, nil)
	return err
}

// UpsertTeam takes a roster, signature and fingerprint to sign the request and attempts to
// create a secret for the given recipient
func (c *Client) UpsertTeam(roster string, rosterSignature string,
	signerFingerprint fpr.Fingerprint) error {

	UpsertTeamRequest := v1structs.UpsertTeamRequest{
		TeamRoster:               roster,
		ArmoredDetachedSignature: rosterSignature,
	}
	request, err := c.newRequest("POST", "teams", UpsertTeamRequest)
	if err != nil {
		return err
	}
	request.Header.Add("authorization", authorization(signerFingerprint))

	_, err = c.do(request, nil)
	return err
}

// ListSecrets for a particular fingerprint.
func (c *Client) ListSecrets(fingerprint fpr.Fingerprint) ([]v1structs.Secret, error) {
	request, err := c.newRequest("GET", "secrets", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("authorization", authorization(fingerprint))
	decodedJSON := new(v1structs.ListSecretsResponse)
	_, err = c.do(request, &decodedJSON)
	if err != nil {
		return nil, err
	}

	return decodedJSON.Secrets, nil
}

// DeleteSecret deletes a secret
func (c *Client) DeleteSecret(fingerprint fpr.Fingerprint, uuid string) error {
	path := fmt.Sprintf("secrets/%s", uuid)
	request, err := c.newRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	request.Header.Add("authorization", authorization(fingerprint))
	_, err = c.do(request, nil)
	return err
}

// UpsertPublicKey creates or updates a public key in the Fluidkeys Directory.
// It requires privateKey to ensure that only the owner of the public key can
// upload it.
func (c *Client) UpsertPublicKey(armoredPublicKey string, privateKey *pgpkey.PgpKey) error {
	armoredSignedJSON, err := makeUpsertPublicKeySignedData(armoredPublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("Failed to create ArmoredSignedJSON: %s", err)
	}
	upsertPublicKeyRequest := v1structs.UpsertPublicKeyRequest{
		ArmoredPublicKey:  armoredPublicKey,
		ArmoredSignedJSON: armoredSignedJSON,
	}
	request, err := c.newRequest("POST", "keys", upsertPublicKeyRequest)
	if err != nil {
		return fmt.Errorf("Failed to upload key: %s", err)
	}
	decodedUpsertResponse := new(v1structs.UpsertPublicKeyResponse)
	_, err = c.do(request, &decodedUpsertResponse)
	return err
}

// GetTeamName attempts to get the team name
func (c *Client) GetTeamName(teamUUID uuid.UUID) (string, error) {
	path := fmt.Sprintf("team/%s", teamUUID)
	request, err := c.newRequest("GET", path, nil)
	if err != nil {
		return "", err
	}
	decodedJSON := new(v1structs.GetTeamResponse)
	response, err := c.do(request, &decodedJSON)
	if err != nil {
		if response != nil && response.StatusCode == http.StatusNotFound {
			return "", ErrTeamNotFound
		}
		return "", err
	}

	return decodedJSON.Name, nil
}

// GetTeamRoster attempts to get the team roster and signature for the given UUID. The API
// responds with encrypted JSON, so it tries to decrypt this using the requestingKey.
func (c *Client) GetTeamRoster(teamUUID uuid.UUID, me fpr.Fingerprint) (
	roster string, signature string, err error) {

	path := fmt.Sprintf("team/%s/roster", teamUUID)
	request, err := c.newRequest("GET", path, nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Add("authorization", authorization(me))
	decodedJSON := new(v1structs.GetTeamRosterResponse)
	response, err := c.do(request, &decodedJSON)
	if err != nil {
		if response == nil {
			return "", "", err
		}
		switch response.StatusCode {
		case http.StatusNotFound:
			return "", "", ErrTeamNotFound

		case http.StatusForbidden:
			return "", "", ErrForbidden

		default:
			return "", "", err
		}
	}

	return decodedJSON.TeamRoster, decodedJSON.ArmoredDetachedSignature, nil
}

// RequestToJoinTeam posts a request to join the team identified by the UUID with the
// given fingerprint and email
func (c *Client) RequestToJoinTeam(
	teamUUID uuid.UUID, fingerprint fpr.Fingerprint, email string) (err error) {

	path := fmt.Sprintf("team/%s/requests-to-join", teamUUID)
	requestToJoinTeamRequest := v1structs.RequestToJoinTeamRequest{TeamEmail: email}

	request, err := c.newRequest("POST", path, requestToJoinTeamRequest)
	if err != nil {
		return err
	}
	request.Header.Add("authorization", authorization(fingerprint))

	response, err := c.do(request, nil)
	if err != nil {
		if response.StatusCode == http.StatusConflict {
			return fmt.Errorf("already got request to join team for %s", email)
		}
		return err
	}

	return nil
}

// ListRequestsToJoinTeam for the team with the given UUID.
func (c *Client) ListRequestsToJoinTeam(teamUUID uuid.UUID, fingerprint fpr.Fingerprint) (
	requestsToJoinTeam []team.RequestToJoinTeam, err error) {

	path := fmt.Sprintf("team/%s/requests-to-join", teamUUID)
	request, err := c.newRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("authorization", authorization(fingerprint))
	decodedJSON := new(v1structs.ListRequestsToJoinTeamResponse)
	_, err = c.do(request, &decodedJSON)
	if err != nil {
		return nil, err
	}
	for _, jsonRequestToJoin := range decodedJSON.Requests {
		requestUUID, err := uuid.FromString(jsonRequestToJoin.UUID)
		if err != nil {
			continue
		}
		requestFingerprint, err := fpr.Parse(jsonRequestToJoin.Fingerprint)
		if err != nil {
			continue
		}

		requestsToJoinTeam = append(requestsToJoinTeam, team.RequestToJoinTeam{
			UUID:        requestUUID,
			TeamUUID:    teamUUID,
			Email:       jsonRequestToJoin.Email,
			Fingerprint: requestFingerprint,
			RequestedAt: time.Time{}, // API doesn't store this
		})
	}

	return requestsToJoinTeam, nil
}

// DeleteRequestToJoinTeam deletes a request to join a team
func (c *Client) DeleteRequestToJoinTeam(teamUUID uuid.UUID, requestUUID uuid.UUID) error {
	path := fmt.Sprintf("team/%s/requests-to-join/%s", teamUUID, requestUUID)
	request, err := c.newRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	_, err = c.do(request, nil)
	return err
}

// Log sends an event to the API. The event is sent in a goroutine so it doesn't block the
// main thread.
func (c *Client) Log(event Event) error {
	if event.Name == "" {
		return fmt.Errorf("invalid event: name can't be empty")
	}

	path := "events"

	var (
		errorText       string
		fingerprintText string
		teamUUIDText    string
	)

	if event.Error != nil {
		errorText = event.Error.Error()
	}

	if event.TeamUUID != nil {
		teamUUIDText = event.TeamUUID.String()
	}

	if event.Fingerprint != nil {
		fingerprintText = event.Fingerprint.Uri()
	}

	requestData := v1structs.CreateEventRequest{
		Name: event.Name,
		RelatedKeyFingerprint: fingerprintText,
		RelatedTeamUUID:       teamUUIDText,
		Error:                 errorText,
	}

	request, err := c.newRequest("POST", path, requestData)
	if err != nil {
		return err
	}
	_, err = c.do(request, nil)
	return err
}

func makeUpsertPublicKeySignedData(armoredPublicKey string, privateKey *pgpkey.PgpKey) (armoredSignedJSON string, err error) {
	publicKeyHash := fmt.Sprintf("%X", sha256.Sum256([]byte(armoredPublicKey)))

	singleTimeUUID, err := uuid.NewV4()
	if err != nil {
		return "", fmt.Errorf("Couldn't generate UUID: %s", err)
	}

	publicKeyData := v1structs.UpsertPublicKeySignedData{
		Timestamp:       time.Now(),
		SingleUseUUID:   singleTimeUUID.String(),
		PublicKeySHA256: publicKeyHash,
	}

	jsonBytes, err := json.Marshal(publicKeyData)
	if err != nil {
		return "", fmt.Errorf("Couldn't marshal JSON: %s", err)
	}

	armoredSignedJSON, err = signText(jsonBytes, privateKey)
	if err != nil {
		return "", fmt.Errorf("Couldn't marshal JSON: %s", err)
	}

	return armoredSignedJSON, nil
}

func signText(bytesToSign []byte, key *pgpkey.PgpKey) (armoredSigned string, err error) {
	armorOutBuffer := bytes.NewBuffer(nil)
	privKey := key.Entity.PrivateKey
	if privKey == nil {
		return "", fmt.Errorf("no private key provided for key %s", key.Fingerprint())
	}
	if privKey.Encrypted {
		return "", fmt.Errorf("private key is encrypted %s", key.Fingerprint())
	}

	armorWriteCloser, err := clearsign.Encode(armorOutBuffer, privKey, nil)
	if err != nil {
		return "", err
	}

	_, err = armorWriteCloser.Write(bytesToSign)
	if err != nil {
		return "", err
	}

	if err := armorWriteCloser.Close(); err != nil {
		return "", err
	}
	return armorOutBuffer.String(), nil
}

func makeErrorForAPIResponse(response *http.Response) error {
	if response.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("Couldn't sign in to API")
	}

	apiErrorResponseDetail := decodeErrorResponse(response)
	if apiErrorResponseDetail != "" {
		return fmt.Errorf("API error: %d %s", response.StatusCode, apiErrorResponseDetail)
	}
	return fmt.Errorf("API error: %d", response.StatusCode)
}

func decodeErrorResponse(response *http.Response) string {
	if response.Body == nil {
		return ""
	}
	errorResponse := v1structs.ErrorResponse{}
	if err := json.NewDecoder(response.Body).Decode(&errorResponse); err != nil {
		return ""
	}
	return errorResponse.Detail
}

// newRequest creates an API request. relativePath is resolved relative to the
// BaseURL of the client.
// If specified, the value pointed to by requestData is JSON encoded and
// included as the request body.
func (c *Client) newRequest(method, relativePath string, requestData interface{}) (*http.Request, error) {
	if !strings.HasSuffix(c.BaseURL.Path, "/") {
		return nil, fmt.Errorf("BaseURL must have a trailing slash, but %q does not", c.BaseURL)
	}
	url, err := c.BaseURL.Parse(relativePath)
	if err != nil {
		return nil, err
	}

	var buf io.ReadWriter
	if requestData != nil {
		buf = new(bytes.Buffer)
		enc := json.NewEncoder(buf)
		err := enc.Encode(requestData)
		if err != nil {
			return nil, err
		}
	}

	request, err := http.NewRequest(method, url.String(), buf)
	if err != nil {
		return nil, err
	}

	if requestData != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.UserAgent != "" {
		request.Header.Set("User-Agent", c.UserAgent)
	}
	return request, nil
}

// do sends an API request and decodes the JSON response, storing it in the
// value pointed to by responseData. If an API error occurs, it returns error.
func (c *Client) do(req *http.Request, responseData interface{}) (response *http.Response, err error) {
	response, err = c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if isSuccess(response.StatusCode) {
		if responseData != nil && isJSON(response) && response.Body != nil {
			if err = json.NewDecoder(response.Body).Decode(responseData); err != nil {
				return nil, err
			}
		}
	} else {
		return response, makeErrorForAPIResponse(response)
	}

	return response, err
}

func isJSON(response *http.Response) bool {
	return response.Header.Get("Content-Type") == "application/json"
}

func isSuccess(httpStatusCode int) bool {
	return httpStatusCode/100 == 2
}

func authorization(fingerprint fpr.Fingerprint) string {
	return "tmpfingerprint: " + fmt.Sprintf("OPENPGP4FPR:%s", fingerprint.Hex())
}
