package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	health "github.com/Financial-Times/go-fthealth/v1_1"
)

type Client interface {
	Do(req *http.Request) (resp *http.Response, err error)
}

type Suggester interface {
	GetSuggestions(payload []byte, tid string) (SuggestionsResponse, error)
	Check() health.Check
}

type FalconSuggester struct {
	FalconSuggestionApiBaseURL string
	FalconSuggestionEndpoint   string
	Client                     Client
}

type SuggestionsResponse struct {
	Suggestions []Suggestion `json:"suggestions"`
}

type Suggestion struct {
	Predicate      string `json:"predicate,omitempty"`
	Id             string `json:"id,omitempty"`
	ApiUrl         string `json:"apiUrl,omitempty"`
	PrefLabel      string `json:"prefLabel,omitempty"`
	SuggestionType string `json:"type,omitempty"`
	IsFTAuthor     bool   `json:"isFTAuthor,omitempty"`
}

func NewSuggester(falconSuggestionApiBaseURL, falconSuggestionEndpoint string, client Client) Suggester {
	return &FalconSuggester{
		FalconSuggestionApiBaseURL: falconSuggestionApiBaseURL,
		FalconSuggestionEndpoint:   falconSuggestionEndpoint,
		Client: client,
	}
}

func (suggester *FalconSuggester) GetSuggestions(payload []byte, tid string) (SuggestionsResponse, error) {
	req, err := http.NewRequest("POST", suggester.FalconSuggestionApiBaseURL+suggester.FalconSuggestionEndpoint, bytes.NewReader(payload))
	if err != nil {
		return SuggestionsResponse{}, err
	}

	req.Header.Add("User-Agent", "UPP draft-suggestion-api")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-Request-Id", tid)

	resp, err := suggester.Client.Do(req)
	if err != nil {
		return SuggestionsResponse{}, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return SuggestionsResponse{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return SuggestionsResponse{}, fmt.Errorf("Falcon Suggestion API returned HTTP %v, body: %v", resp.StatusCode, string(body))
	}

	var response SuggestionsResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return SuggestionsResponse{}, err
	}
	return response, nil
}

func (suggester *FalconSuggester) Check() health.Check {
	return health.Check{
		ID:               "falcon-suggestion-api",
		BusinessImpact:   "Suggestions from TME won't work",
		Name:             "Falcon Suggestion API Healthcheck",
		PanicGuide:       "https://dewey.in.ft.com/view/system/draft-suggestion-api",
		Severity:         2,
		TechnicalSummary: "Falcon Suggestion API is not available",
		Checker:          suggester.healthCheck,
	}
}

func (suggester *FalconSuggester) healthCheck() (string, error) {
	req, err := http.NewRequest("GET", suggester.FalconSuggestionApiBaseURL+"/__gtg", nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("User-Agent", "UPP draft-suggestion-api")

	resp, err := suggester.Client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Health check returned a non-200 HTTP status: %v", resp.StatusCode)
	}
	return "Falcon Suggestion API is healthy", nil
}