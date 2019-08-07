package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Financial-Times/go-fthealth/v1_1"
	log "github.com/Financial-Times/go-logger"
	"github.com/Financial-Times/public-suggestions-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	personType = "http://www.ft.com/ontology/person/Person"
)

func init() {
	log.InitLogger("handler_test", "ERROR")
}

type mockHttpClient struct {
	mock.Mock
}

func (c *mockHttpClient) Do(req *http.Request) (resp *http.Response, err error) {
	args := c.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

type ClosingBuffer struct {
	*bytes.Buffer
}

func (cb *ClosingBuffer) Close() (err error) {
	// do nothing
	return
}

type mockSuggesterService struct {
	mock.Mock
}

type faultyReader struct {
}

func (r *faultyReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("Reading error")
}

func (r *faultyReader) Close() error {
	return nil
}

func (s *mockSuggesterService) GetSuggestions(payload []byte, tid string, flags service.Flags) (service.SuggestionsResponse, error) {
	args := s.Called(payload, tid, flags)
	return args.Get(0).(service.SuggestionsResponse), args.Error(1)
}

func (s *mockSuggesterService) FilterSuggestions(suggestions []service.Suggestion) []service.Suggestion {
	args := s.Called(suggestions)
	return args.Get(0).([]service.Suggestion)
}

func (s *mockSuggesterService) GetName() string {
	return "Mock suggester service"
}

func (s *mockSuggesterService) Check() v1_1.Check {
	args := s.Called()
	return args.Get(0).(v1_1.Check)
}

func TestRequestHandler_HandleSuggestionSuccessfully(t *testing.T) {
	expect := assert.New(t)

	body := []byte(`{"id":"http://api.ft.com/things/9d5e441e-0b02-11e8-8eb7-42f857ea9f0","byline":"Test byline","bodyXML":"Test body","title":"Test title"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	expectedResp := service.SuggestionsResponse{Suggestions: []service.Suggestion{
		{
			Concept: service.Concept{
				IsFTAuthor: true,
				ID:         "authors-suggestion-api",
				APIURL:     "apiurl2",
				PrefLabel:  "prefLabel2",
				Type:       personType,
			},
		},
	}}

	mockClient := new(mockHttpClient)
	mockPublicThings := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	mockInternalConcResp := service.ConcordanceResponse{
		Concepts: make(map[string]service.Concept),
	}
	mockInternalConcResp.Concepts["authors-suggestion-api"] = service.Concept{
		IsFTAuthor: true, ID: "authors-suggestion-api", APIURL: "apiurl2", PrefLabel: "prefLabel2", Type: personType,
	}
	expectedBody, err := json.Marshal(&mockInternalConcResp)
	require.NoError(t, err)
	buffer := &ClosingBuffer{
		Buffer: bytes.NewBuffer(expectedBody),
	}
	mockClient.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{Body: buffer, StatusCode: http.StatusOK}, nil)

	mockPublicThings.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{Body: ioutil.NopCloser(strings.NewReader("")), StatusCode: http.StatusOK}, nil)
	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	mockSuggester.On("GetSuggestions", body, "tid_test", service.Flags{}).Return(expectedResp, nil).Once()
	mockSuggester.On("FilterSuggestions", expectedResp.Suggestions, mock.Anything).Return(expectedResp.Suggestions).Once()
	mockSuggester.On("GetSuggestions", body, "tid_test", service.Flags{}).Return(service.SuggestionsResponse{}, nil)

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusOK, w.Code)
	expect.Equal(`{"suggestions":[{"id":"authors-suggestion-api","apiUrl":"apiurl2","type":"http://www.ft.com/ontology/person/Person","prefLabel":"prefLabel2","isFTAuthor":true}]}`, w.Body.String())

	mockSuggester.AssertExpectations(t)
	mockPublicThings.AssertExpectations(t)
	mockClient.AssertExpectations(t)
}

func TestRequestHandler_HandleSuggestionErrorOnRequestRead(t *testing.T) {
	expect := assert.New(t)

	req := httptest.NewRequest("POST", "/content/suggest", &faultyReader{})
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusBadRequest, w.Code)
	expect.Equal(`{"message": "Error while reading payload"}`, w.Body.String())

	mockSuggester.AssertExpectations(t)    //no calls
	mockPublicThings.AssertExpectations(t) // no calls
	mockClient.AssertExpectations(t)       //no calls
}

func TestRequestHandler_HandleSuggestionEmptyBody(t *testing.T) {
	expect := assert.New(t)

	body := []byte("")
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusBadRequest, w.Code)
	expect.Equal(`{"message": "Payload should be a non-empty JSON object"}`, w.Body.String())

	mockSuggester.AssertExpectations(t)    //no calls
	mockPublicThings.AssertExpectations(t) //no calls
	mockClient.AssertExpectations(t)       //no calls
}

func TestRequestHandler_HandleSuggestionEmptyJsonRequest(t *testing.T) {
	expect := assert.New(t)

	body := []byte("{}")
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusBadRequest, w.Code)
	expect.Equal(`{"message": "Payload should be a non-empty JSON object"}`, w.Body.String())

	mockSuggester.AssertExpectations(t)    //no calls
	mockPublicThings.AssertExpectations(t) //no calls
	mockClient.AssertExpectations(t)       //no calls
}

func TestRequestHandler_HandleSuggestionErrorOnGetSuggestions(t *testing.T) {
	expect := assert.New(t)

	body := []byte(`{"bodyXML":"Test body"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	mockSuggester.On("GetSuggestions", body, "tid_test", service.Flags{}).Return(service.SuggestionsResponse{Suggestions: []service.Suggestion{}}, errors.New("Timeout error"))

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusOK, w.Code)
	expect.Equal(`{"suggestions":[]}`, w.Body.String())

	mockSuggester.AssertExpectations(t)
	mockPublicThings.AssertExpectations(t) //no calls
	mockClient.AssertExpectations(t)       //no calls
}

func TestRequestHandler_HandleSuggestionOkWhenNoContentSuggestions(t *testing.T) {
	expect := assert.New(t)
	body := []byte(`{"bodyXML":"Test body"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	service.NoContentError = errors.New("No content error")
	mockSuggester.On("GetSuggestions", body, "tid_test", service.Flags{}).Return(service.SuggestionsResponse{
		Suggestions: make([]service.Suggestion, 0),
	}, service.NoContentError)

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusOK, w.Code)
	expect.Equal(`{"suggestions":[]}`, w.Body.String())

	mockSuggester.AssertExpectations(t)
	mockPublicThings.AssertExpectations(t) //no calls
	mockClient.AssertExpectations(t)       //no calls
}

//Might not happen at all if MetadataServices returns always 204 when there are no suggestions
func TestRequestHandler_HandleSuggestionOkWhenEmptySuggestions(t *testing.T) {
	expect := assert.New(t)
	body := []byte(`{"byline":"Test byline","bodyXML":"Test body","title":"Test title"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	mockSuggester.On("GetSuggestions", body, "tid_test", service.Flags{}).Return(service.SuggestionsResponse{Suggestions: []service.Suggestion{}}, nil)

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusOK, w.Code)
	expect.Equal(`{"suggestions":[]}`, w.Body.String())

	mockSuggester.AssertExpectations(t)
	mockPublicThings.AssertExpectations(t) //no calls
	mockClient.AssertExpectations(t)       //no calls
}

func TestRequestHandler_HandleSuggestionErrorOnGetConcordance(t *testing.T) {
	expect := assert.New(t)

	body := []byte(`{"bodyXML":"Test body"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockClient := new(mockHttpClient)
	mockSuggester := new(mockSuggesterService)
	mockPublicThings := new(mockHttpClient)
	mockConcordance := &service.ConcordanceService{ConcordanceBaseURL: "concordanceBaseURL", ConcordanceEndpoint: "concordanceEndpoint", Client: mockClient}

	mockSuggester.On("GetSuggestions", body, "tid_test", service.Flags{}).Return(service.SuggestionsResponse{Suggestions: []service.Suggestion{
		{
			Concept: service.Concept{
				IsFTAuthor: true,
				ID:         "authors-suggestion-api",
				APIURL:     "apiurl2",
				PrefLabel:  "prefLabel2",
				Type:       personType,
			},
		},
	}}, nil)
	mockClient.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{}, errors.New("Timeout error"))

	broaderService := &service.BroaderConceptsProvider{
		Client: mockPublicThings,
	}

	blacklisterMock := new(mockHttpClient)
	blacklisterMock.On("Do", mock.AnythingOfType("*http.Request")).Return(&http.Response{
		Body: ioutil.NopCloser(strings.NewReader(
			`{"uuids":[]}`)),
		StatusCode: http.StatusOK,
	}, nil)
	blacklister := service.NewConceptBlacklister("blacklisterUrl", "blacklisterEndpoint", blacklisterMock)

	handler := NewRequestHandler(&service.AggregateSuggester{
		Concordance:     mockConcordance,
		Suggesters:      []service.Suggester{mockSuggester},
		BroaderProvider: broaderService,
		Blacklister:     blacklister,
	})
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusServiceUnavailable, w.Code)
	expect.Equal(`{"message": "aggregating suggestions failed!"}`, w.Body.String())

	mockSuggester.AssertExpectations(t)
	mockPublicThings.AssertExpectations(t)
	mockClient.AssertExpectations(t)
}
