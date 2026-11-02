package control

import (
	"testing"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func TestTelegramMessageValuesIncludeTopic(t *testing.T) {
	settings := &model.Settings{TelegramChatID: "-100123", TelegramMessageThreadID: 42}
	values := telegramMessageValues(settings, "hello")

	if values.Get("chat_id") != "-100123" || values.Get("text") != "hello" {
		t.Fatalf("unexpected message values: %v", values)
	}
	if values.Get("message_thread_id") != "42" {
		t.Fatalf("Topic ID is missing: %v", values)
	}
}

func TestTelegramMessageValuesOmitEmptyTopic(t *testing.T) {
	values := telegramMessageValues(&model.Settings{TelegramChatID: "123"}, "hello")
	if values.Has("message_thread_id") {
		t.Fatalf("empty Topic ID must not be sent: %v", values)
	}
}
