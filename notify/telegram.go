package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// telegram implements Notifier via the Telegram Bot API.
type telegram struct {
	botToken string
	chatID   string
	events   map[string]bool
	client   *http.Client // shared client for connection reuse
}

func (t *telegram) ShouldNotify(event string) bool {
	return t.events[event]
}

func (t *telegram) Send(event string, message string) {
	if !t.ShouldNotify(event) {
		return
	}

	// Fire and forget in a goroutine to not block workers.
	go func() {
		if err := t.send(message); err != nil {
			fmt.Printf("[notify] Telegram send failed: %v\n", err)
		}
	}()
}

func (t *telegram) send(message string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling payload: %w", err)
	}

	resp, err := t.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}
