package message

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

type CreateMessageParams struct {
	Role             MessageRole
	Parts            []ContentPart
	Model            string
	Provider         string
	IsSummaryMessage bool
	IsPlanMode       bool
}

// MessageCursor identifies a position in the message timeline for keyset
// pagination. The zero value means "from the newest message".
type MessageCursor struct {
	CreatedAt int64
	ID        string
}

// MessagePage holds a page of messages plus pagination metadata.
type MessagePage struct {
	Messages []Message
	HasMore  bool
	Cursor   MessageCursor // position of the oldest message in this page
}

type Service interface {
	pubsub.Subscriber[Message]
	pubsub.Publisher[Message]
	Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error)
	Update(ctx context.Context, message Message) error
	Persist(ctx context.Context, message Message) error
	Get(ctx context.Context, id string) (Message, error)
	List(ctx context.Context, sessionID string) ([]Message, error)
	ListRecent(ctx context.Context, sessionID string, limit int) (MessagePage, error)
	ListBefore(ctx context.Context, sessionID string, cursor MessageCursor, limit int) (MessagePage, error)
	ListUserMessages(ctx context.Context, sessionID string) ([]Message, error)
	ListAllUserMessages(ctx context.Context) ([]Message, error)
	Delete(ctx context.Context, id string) error
	DeleteSessionMessages(ctx context.Context, sessionID string) error
}

type service struct {
	*pubsub.Broker[Message]
	q db.Querier
}

func NewService(q db.Querier) Service {
	return &service{
		Broker: pubsub.NewBroker[Message](),
		q:      q,
	}
}

func (s *service) Delete(ctx context.Context, id string) error {
	message, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	err = s.q.DeleteMessage(ctx, message.ID)
	if err != nil {
		return err
	}
	// Clone the message before publishing to avoid race conditions with
	// concurrent modifications to the Parts slice.
	s.Publish(pubsub.DeletedEvent, message.Clone())
	return nil
}

func (s *service) Create(ctx context.Context, sessionID string, params CreateMessageParams) (Message, error) {
	if params.Role != Assistant {
		params.Parts = append(params.Parts, Finish{
			Reason: "stop",
		})
	}
	partsJSON, err := marshalParts(params.Parts)
	if err != nil {
		return Message{}, err
	}
	isSummary := int64(0)
	if params.IsSummaryMessage {
		isSummary = 1
	}
	isPlanMode := int64(0)
	if params.IsPlanMode {
		isPlanMode = 1
	}
	dbMessage, err := s.q.CreateMessage(ctx, db.CreateMessageParams{
		ID:               uuid.New().String(),
		SessionID:        sessionID,
		Role:             string(params.Role),
		Parts:            string(partsJSON),
		Model:            sql.NullString{String: string(params.Model), Valid: true},
		Provider:         sql.NullString{String: params.Provider, Valid: params.Provider != ""},
		IsSummaryMessage: isSummary,
		IsPlanMode:       isPlanMode,
	})
	if err != nil {
		return Message{}, err
	}
	message, err := s.fromDBItem(dbMessage)
	if err != nil {
		return Message{}, err
	}
	// Clone the message before publishing to avoid race conditions with
	// concurrent modifications to the Parts slice.
	s.Publish(pubsub.CreatedEvent, message.Clone())
	return message, nil
}

func (s *service) DeleteSessionMessages(ctx context.Context, sessionID string) error {
	messages, err := s.List(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.SessionID == sessionID {
			err = s.Delete(ctx, message.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) Update(ctx context.Context, message Message) error {
	if err := s.Persist(ctx, message); err != nil {
		return err
	}
	message.UpdatedAt = time.Now().Unix()
	s.Publish(pubsub.UpdatedEvent, message.Clone())
	return nil
}

// Persist writes the message to the DB without publishing a pubsub event.
func (s *service) Persist(ctx context.Context, message Message) error {
	parts, err := marshalParts(message.Parts)
	if err != nil {
		return err
	}
	finishedAt := sql.NullInt64{}
	if f := message.FinishPart(); f != nil {
		finishedAt.Int64 = f.Time
		finishedAt.Valid = true
	}
	return s.q.UpdateMessage(ctx, db.UpdateMessageParams{
		ID:         message.ID,
		Parts:      string(parts),
		FinishedAt: finishedAt,
	})
}

func (s *service) Get(ctx context.Context, id string) (Message, error) {
	dbMessage, err := s.q.GetMessage(ctx, id)
	if err != nil {
		return Message{}, err
	}
	return s.fromDBItem(dbMessage)
}

func (s *service) List(ctx context.Context, sessionID string) ([]Message, error) {
	dbMessages, err := s.q.ListMessagesBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMessage := range dbMessages {
		messages[i], err = s.fromDBItem(dbMessage)
		if err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (s *service) ListUserMessages(ctx context.Context, sessionID string) ([]Message, error) {
	dbMessages, err := s.q.ListUserMessagesBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMessage := range dbMessages {
		messages[i], err = s.fromDBItem(dbMessage)
		if err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (s *service) ListAllUserMessages(ctx context.Context) ([]Message, error) {
	dbMessages, err := s.q.ListAllUserMessages(ctx)
	if err != nil {
		return nil, err
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMessage := range dbMessages {
		messages[i], err = s.fromDBItem(dbMessage)
		if err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (s *service) ListRecent(ctx context.Context, sessionID string, limit int) (MessagePage, error) {
	if limit <= 0 {
		return MessagePage{}, nil
	}
	dbMessages, err := s.q.ListRecentMessagesBySession(ctx, db.ListRecentMessagesBySessionParams{
		SessionID: sessionID,
		Limit:     int64(limit + 1),
	})
	if err != nil {
		return MessagePage{}, err
	}
	hasMore := len(dbMessages) > limit
	if hasMore {
		dbMessages = dbMessages[:limit]
	}
	// Reverse to chronological order (ASC).
	for i, j := 0, len(dbMessages)-1; i < j; i, j = i+1, j-1 {
		dbMessages[i], dbMessages[j] = dbMessages[j], dbMessages[i]
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMsg := range dbMessages {
		messages[i], err = s.fromDBItem(dbMsg)
		if err != nil {
			return MessagePage{}, err
		}
	}
	var cursor MessageCursor
	if len(messages) > 0 {
		oldest := messages[0]
		cursor = MessageCursor{CreatedAt: oldest.CreatedAt, ID: oldest.ID}
	}
	return MessagePage{Messages: messages, HasMore: hasMore, Cursor: cursor}, nil
}

func (s *service) ListBefore(ctx context.Context, sessionID string, cursor MessageCursor, limit int) (MessagePage, error) {
	if limit <= 0 {
		return MessagePage{}, nil
	}
	dbMessages, err := s.q.ListMessagesBySessionBefore(ctx, db.ListMessagesBySessionBeforeParams{
		SessionID: sessionID,
		ID:        cursor.ID,
		Limit:     int64(limit + 1),
	})
	if err != nil {
		return MessagePage{}, err
	}
	hasMore := len(dbMessages) > limit
	if hasMore {
		dbMessages = dbMessages[:limit]
	}
	// Reverse to chronological order (ASC).
	for i, j := 0, len(dbMessages)-1; i < j; i, j = i+1, j-1 {
		dbMessages[i], dbMessages[j] = dbMessages[j], dbMessages[i]
	}
	messages := make([]Message, len(dbMessages))
	for i, dbMsg := range dbMessages {
		messages[i], err = s.fromDBItem(dbMsg)
		if err != nil {
			return MessagePage{}, err
		}
	}
	var newCursor MessageCursor
	if len(messages) > 0 {
		oldest := messages[0]
		newCursor = MessageCursor{CreatedAt: oldest.CreatedAt, ID: oldest.ID}
	}
	return MessagePage{Messages: messages, HasMore: hasMore, Cursor: newCursor}, nil
}

func (s *service) fromDBItem(item db.Message) (Message, error) {
	parts, err := unmarshalParts([]byte(item.Parts))
	if err != nil {
		return Message{}, err
	}
	return Message{
		ID:               item.ID,
		SessionID:        item.SessionID,
		Role:             MessageRole(item.Role),
		Parts:            parts,
		Model:            item.Model.String,
		Provider:         item.Provider.String,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
		IsSummaryMessage: item.IsSummaryMessage != 0,
		IsPlanMode:       item.IsPlanMode != 0,
	}, nil
}

type partType string

const (
	reasoningType  partType = "reasoning"
	textType       partType = "text"
	imageURLType   partType = "image_url"
	binaryType     partType = "binary"
	toolCallType   partType = "tool_call"
	toolResultType partType = "tool_result"
	finishType     partType = "finish"
)

type partWrapper struct {
	Type partType    `json:"type"`
	Data ContentPart `json:"data"`
}

func marshalParts(parts []ContentPart) ([]byte, error) {
	wrappedParts := make([]partWrapper, len(parts))

	for i, part := range parts {
		var typ partType

		switch part.(type) {
		case ReasoningContent:
			typ = reasoningType
		case TextContent:
			typ = textType
		case ImageURLContent:
			typ = imageURLType
		case BinaryContent:
			typ = binaryType
		case ToolCall:
			typ = toolCallType
		case ToolResult:
			typ = toolResultType
		case Finish:
			typ = finishType
		default:
			return nil, fmt.Errorf("unknown part type: %T", part)
		}

		wrappedParts[i] = partWrapper{
			Type: typ,
			Data: part,
		}
	}
	return json.Marshal(wrappedParts)
}

func unmarshalParts(data []byte) ([]ContentPart, error) {
	temp := []json.RawMessage{}

	if err := json.Unmarshal(data, &temp); err != nil {
		return nil, err
	}

	parts := make([]ContentPart, 0)

	for _, rawPart := range temp {
		var wrapper struct {
			Type partType        `json:"type"`
			Data json.RawMessage `json:"data"`
		}

		if err := json.Unmarshal(rawPart, &wrapper); err != nil {
			return nil, err
		}

		switch wrapper.Type {
		case reasoningType:
			part := ReasoningContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case textType:
			part := TextContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case imageURLType:
			part := ImageURLContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case binaryType:
			part := BinaryContent{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case toolCallType:
			part := ToolCall{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case toolResultType:
			part := ToolResult{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case finishType:
			part := Finish{}
			if err := json.Unmarshal(wrapper.Data, &part); err != nil {
				return nil, err
			}
			parts = append(parts, part)
		default:
			return nil, fmt.Errorf("unknown part type: %s", wrapper.Type)
		}
	}

	return parts, nil
}
