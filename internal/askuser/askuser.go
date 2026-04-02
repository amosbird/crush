package askuser

import (
	"context"
	"encoding/json"
	"time"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// Option represents a selectable choice.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionRequest is published via pubsub to notify the UI.
type QuestionRequest struct {
	ID         string   `json:"id"`
	SessionID  string   `json:"session_id"`
	ToolCallID string   `json:"tool_call_id"`
	ToolName   string   `json:"tool_name,omitempty"`
	Question   string   `json:"question"`
	Header     string   `json:"header,omitempty"`
	Body       string   `json:"body,omitempty"`
	Options    []Option `json:"options,omitempty"`
	Multi      bool     `json:"multi,omitempty"`
	AllowText  bool     `json:"allow_text"`
}

// QuestionResponse is the user's answer.
type QuestionResponse struct {
	RequestID string   `json:"request_id"`
	Answers   []string `json:"answers"`
}

// Service manages ask_user interactions between the agent and the UI.
type Service interface {
	pubsub.Subscriber[QuestionRequest]
	Ask(ctx context.Context, req QuestionRequest) ([]string, error)
	Respond(requestID string, answers []string)
}

type service struct {
	*pubsub.Broker[QuestionRequest]

	pendingRequests *csync.Map[string, chan []string]
}

// NewService creates a new ask_user service.
func NewService() Service {
	return &service{
		Broker:          pubsub.NewBroker[QuestionRequest](),
		pendingRequests: csync.NewMap[string, chan []string](),
	}
}

// Ask publishes a question to the UI and blocks until the user responds.
func (s *service) Ask(ctx context.Context, req QuestionRequest) ([]string, error) {
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	respCh := make(chan []string, 1)
	s.pendingRequests.Set(req.ID, respCh)
	defer s.pendingRequests.Del(req.ID)

	s.Publish(pubsub.CreatedEvent, req)

	// Re-publish periodically in case the event was dropped under
	// pipeline backpressure (e.g. during heavy streaming).
	retryTicker := time.NewTicker(500 * time.Millisecond)
	defer retryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case answers := <-respCh:
			return answers, nil
		case <-retryTicker.C:
			s.Publish(pubsub.CreatedEvent, req)
		}
	}
}

// Respond sends the user's answers back to the blocked Ask call.
func (s *service) Respond(requestID string, answers []string) {
	respCh, ok := s.pendingRequests.Get(requestID)
	if ok {
		respCh <- answers
	}
}

// MarshalAnswers serializes answers to a display string.
func MarshalAnswers(answers []string) string {
	if len(answers) == 1 {
		return answers[0]
	}
	b, _ := json.Marshal(answers)
	return string(b)
}
