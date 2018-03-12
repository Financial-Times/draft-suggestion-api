package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Financial-Times/draft-suggestion-api/service"
	"github.com/Financial-Times/go-fthealth/v1_1"
	log "github.com/Financial-Times/go-logger"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func init() {
	log.InitLogger("handler_test", "ERROR")
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

func (s *mockSuggesterService) GetSuggestions(payload []byte, tid string) (service.SuggestionsResponse, error) {
	args := s.Called(payload, tid)
	return args.Get(0).(service.SuggestionsResponse), args.Error(1)
}

func (s *mockSuggesterService) Check() v1_1.Check {
	args := s.Called()
	return args.Get(0).(v1_1.Check)
}

func TestRequestHandler_HandleSuggestionSuccessfully(t *testing.T) {
	expect := assert.New(t)

	body := []byte(`{"bodyXml": "Test"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	expectedResp := service.SuggestionsResponse{Suggestions: []service.Suggestion{{PrefLabel: "TestMan"}}}
	mockSuggester := new(mockSuggesterService)
	mockSuggester.On("GetSuggestions", body, "tid_test").Return(expectedResp, nil)

	handler := NewRequestHandler(mockSuggester)
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusOK, w.Code)
	expect.Equal(`{"suggestions":[{"prefLabel":"TestMan"}]}`, w.Body.String())
	mockSuggester.AssertExpectations(t)
}

func TestRequestHandler_HandleSuggestionErrorOnRequestRead(t *testing.T) {
	expect := assert.New(t)

	req := httptest.NewRequest("POST", "/content/suggest", &faultyReader{})
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockSuggester := new(mockSuggesterService)

	handler := NewRequestHandler(mockSuggester)
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusBadRequest, w.Code)
	expect.Equal(`{"message": "Error by reading payload"}`, w.Body.String())
	mockSuggester.AssertExpectations(t) //no calls
}

func TestRequestHandler_HandleSuggestionEmptyBody(t *testing.T) {
	expect := assert.New(t)

	body := []byte("")
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockSuggester := new(mockSuggesterService)

	handler := NewRequestHandler(mockSuggester)
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusBadRequest, w.Code)
	expect.Equal(`{"message": "Payload should not be empty"}`, w.Body.String())
	mockSuggester.AssertExpectations(t) //no calls
}

func TestRequestHandler_HandleSuggestionErrorOnGetSuggestions(t *testing.T) {
	expect := assert.New(t)

	body := []byte(`{"bodyXml": "Test"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockSuggester := new(mockSuggesterService)
	mockSuggester.On("GetSuggestions", body, "tid_test").Return(service.SuggestionsResponse{}, errors.New("Timeout error"))

	handler := NewRequestHandler(mockSuggester)
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusServiceUnavailable, w.Code)
	expect.Equal(`{"message": "Requesting suggestions failed"}`, w.Body.String())
	mockSuggester.AssertExpectations(t)
}

func TestRequestHandler_HandleSuggestionEmptySuggestions(t *testing.T) {
	expect := assert.New(t)

	body := []byte(`{"bodyXml": "Test"}`)
	req := httptest.NewRequest("POST", "/content/suggest", bytes.NewReader(body))
	req.Header.Add("X-Request-Id", "tid_test")
	w := httptest.NewRecorder()

	mockSuggester := new(mockSuggesterService)
	mockSuggester.On("GetSuggestions", body, "tid_test").Return(service.SuggestionsResponse{Suggestions: []service.Suggestion{}}, nil)

	handler := NewRequestHandler(mockSuggester)
	handler.HandleSuggestion(w, req)

	expect.Equal(http.StatusOK, w.Code)
	expect.Equal(`{"suggestions":[]}`, w.Body.String())
	mockSuggester.AssertExpectations(t)
}