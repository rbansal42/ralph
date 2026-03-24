package notify

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Event types for notifications.
const (
	EventStart    = "start"
	EventComplete = "complete"
	EventError    = "error"
	EventStall    = "stall"
)

// Notifier sends notifications on key events.
type Notifier interface {
	// Send sends a notification message. Non-blocking — errors are logged, not returned.
	Send(event string, message string)
	// ShouldNotify returns true if this event type should trigger a notification.
	ShouldNotify(event string) bool
}

// noop is a no-op notifier used when notifications are disabled.
type noop struct{}

func (n *noop) Send(event string, message string) {}
func (n *noop) ShouldNotify(event string) bool    { return false }

// New creates a Notifier based on config. Returns a no-op notifier if not configured.
func New(botToken, chatID string, notifyOn []string) Notifier {
	if botToken == "" || chatID == "" {
		return &noop{}
	}

	// Resolve env var references (e.g. "$TELEGRAM_BOT_TOKEN")
	botToken = resolveEnvVar(botToken)
	chatID = resolveEnvVar(chatID)

	if botToken == "" || chatID == "" {
		fmt.Println("[notify] Telegram not configured (token or chat ID missing after env resolution)")
		return &noop{}
	}

	// Default: notify on all events
	events := make(map[string]bool)
	if len(notifyOn) == 0 {
		events[EventStart] = true
		events[EventComplete] = true
		events[EventError] = true
		events[EventStall] = true
	} else {
		for _, e := range notifyOn {
			events[e] = true
		}
	}

	return &telegram{
		botToken: botToken,
		chatID:   chatID,
		events:   events,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func resolveEnvVar(val string) string {
	if strings.HasPrefix(val, "$") {
		envName := strings.TrimPrefix(val, "$")
		return os.Getenv(envName) // returns "" if env var is unset
	}
	return val
}
