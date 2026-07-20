package externalmock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"
)

// Telegram Bot API response structures match the real Telegram Bot API v7.11.
// Ref: https://core.telegram.org/bots/api

// TGMockPort is the fixed port for Docker Compose containers.
const TGMockPort = ":19003"

// TGUser matches the real Telegram User object.
type TGUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

// TGChat matches the real Telegram Chat object.
type TGChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// TGMessage matches the real Telegram Message object from the Bot API.
type TGMessage struct {
	MessageID int     `json:"message_id"`
	From      *TGUser `json:"from,omitempty"`
	Chat      TGChat  `json:"chat"`
	Date      int64   `json:"date"`
	Text      string  `json:"text,omitempty"`
}

// TGResponse matches the real Telegram API response envelope.
type TGResponse struct {
	OK     bool       `json:"ok"`
	Result *TGMessage `json:"result,omitempty"`
}

func newTelegramHandler(log *RequestLog) http.HandlerFunc {
	var msgID int
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body string
		if r.Body != nil && r.ContentLength > 0 {
			b := make([]byte, r.ContentLength)
			r.Body.Read(b)
			body = string(b)
		}
		log.Record("telegram", r, body)

		msgID++
		json.NewEncoder(w).Encode(TGResponse{
			OK: true,
			Result: &TGMessage{
				MessageID: msgID,
				From:      &TGUser{ID: 7890, IsBot: true, FirstName: "FlowgentBot", Username: "flowgent_bot"},
				Chat:      TGChat{ID: -456789, Type: "group"},
				Date:      time.Now().Unix(),
				Text:      "notification delivered",
			},
		})
	}
}

// NewMockTelegramAPI returns an httptest server speaking the real Telegram Bot API.
func NewMockTelegramAPI(log *RequestLog) *httptest.Server {
	return httptest.NewServer(newTelegramHandler(log))
}

// NewFixedPortTelegramAPI listens on TGMockPort so notification containers
// can reach the mock via host.docker.internal.
func NewFixedPortTelegramAPI(log *RequestLog) *http.Server {
	srv := &http.Server{Addr: TGMockPort, Handler: newTelegramHandler(log)}
	go func() { _ = srv.ListenAndServe() }()
	return srv
}
