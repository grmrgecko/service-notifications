package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/slack-go/slack"
	"gorm.io/gorm"
)

// Commonly used strings.
const (
	APIOK         = "ok"
	APIERR        = "error"
	APIForbidden  = "Forbidden"
	APINoEndpoint = "No endpoint found"
)

// Main response structure.
type APIGeneralResp struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// Typical API responses are done with JSON. To make it easier to respond, this function will marshal/send json to a response writer.
func (s *HTTPServer) JSONResponse(w http.ResponseWriter, resp interface{}) {
	// Encode response as json.
	js, err := json.Marshal(resp)
	if err != nil {
		// Error should not happen normally...
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// If no error, we can set content type header and send response.
	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
	w.Write([]byte{'\n'})
}

// There are quite a few request that send a general response on error. This function is to make it easy to build/send a general response.
func (s *HTTPServer) APISendGeneralResp(w http.ResponseWriter, status, err string) {
	resp := APIGeneralResp{}
	resp.Status = status
	resp.Error = err
	s.JSONResponse(w, resp)
}

// Verifies that the client connectiong is authenticated.
func (s *HTTPServer) APIAuthenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")

		if s.config.APIKey != "" && s.config.APIKey != apiKey {
			s.APISendGeneralResp(w, APIERR, APIForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Setup HTTP router with routes for the API calls.
func (s *HTTPServer) RegisterAPIRoutes(r *mux.Router) {
	api := r.PathPrefix("/api").Subrouter()

	// Requires authentication.
	api.Use(s.APIAuthenticationMiddleware)

	// Just a test call.
	api.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		s.APISendGeneralResp(w, APIOK, "")
	})

	// Send message to slack channel for the current service.
	// Defaults to admin if no service currently occuring.
	api.HandleFunc("/send_message", func(w http.ResponseWriter, r *http.Request) {
		// Get message, either from URL query or multi part form.
		var message string
		err := r.ParseMultipartForm(32 << 20) // maxMemory 32MB
		if err == nil {
			message = r.Form.Get("message")
		}
		if message == "" {
			message = r.URL.Query().Get("message")
		}

		// If no message provided, fail.
		if message == "" {
			log.Println("No message provided")
			s.APISendGeneralResp(w, APIERR, "No message provided")
			return
		}

		// Get current time and default conversation.
		now := time.Now().UTC()
		conversation := app.config.Slack.DefaultConversation

		// Find plan times that are occuring right now. A 60-minute buffer
		// is applied past ends_at so services that run long still resolve
		// to their event channel instead of falling back to the default.
		// Order by starts_at DESC so the most recent active service wins
		// when a later service's window overlaps an earlier service's buffer.
		var planTime PlanTimes
		err = app.db.Where("time_type='service' AND starts_at < ? AND ends_at > ?", now, now.Add(-60*time.Minute)).Order("starts_at DESC").First(&planTime).Error
		// A "record not found" simply means no service is occuring right now, in
		// which case we fall back to the default conversation. Any other error
		// (e.g. "database is locked") must NOT be swallowed: treating it as "no
		// service" would silently misroute the message to the default
		// conversation instead of the event channel. Fail so the caller retries.
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Println("Error looking up plan time:", err)
			s.APISendGeneralResp(w, APIERR, "Error looking up plan time")
			return
		}
		if planTime.Plan != 0 {
			// If plan found, check for the slack channel.
			var channel SlackChannels
			err = app.db.Where("pc_plan = ?", planTime.Plan).First(&channel).Error
			// As above, only "record not found" is a benign result here. On any
			// other error we must not fall through to the default conversation.
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Println("Error looking up slack channel:", err)
				s.APISendGeneralResp(w, APIERR, "Error looking up slack channel")
				return
			}
			if channel.ID != "" {
				// If slack channel found, update the conversation to the channel ID.
				conversation = channel.ID
			}
		}

		// If no conversation found, likely will happen if no admin is configured, return error.
		if conversation == "" {
			log.Println("No conversation found")
			s.APISendGeneralResp(w, APIERR, "No conversation found")
			return
		}

		// Send message to Slack.
		_, _, err = app.slack.PostMessage(conversation, slack.MsgOptionText(message, false))
		if err != nil {
			log.Println("Error sending message:", err)
			s.APISendGeneralResp(w, APIERR, "Error sending message")
			return
		}

		// Return a success.
		s.APISendGeneralResp(w, APIOK, "")
	}).Methods(http.MethodPost)

	// If nothing else, we return a not found response.
	api.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.APISendGeneralResp(w, APIERR, APINoEndpoint)
	})
}
