package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

type telegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      struct {
		Username string `json:"username"`
	} `json:"result"`
}

func telegramHTTPClient(proxyValue string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyValue != "" {
		proxyURL, err := url.Parse(proxyValue)
		if err != nil || proxyURL.Host == "" || (proxyURL.Scheme != "http" && proxyURL.Scheme != "https") {
			return nil, fmt.Errorf("invalid Telegram proxy URL")
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func telegramCall(ctx context.Context, settings *model.Settings, method string, values url.Values) (*telegramResponse, error) {
	client, err := telegramHTTPClient(settings.TelegramProxyURL)
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", settings.TelegramBotToken, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var result telegramResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !result.OK {
		message := result.Description
		if message == "" {
			message = response.Status
		}
		return nil, fmt.Errorf("Telegram: %s", message)
	}
	return &result, nil
}

func sendTelegramMessage(ctx context.Context, settings *model.Settings, message string) error {
	_, err := telegramCall(ctx, settings, "sendMessage", url.Values{
		"chat_id": {settings.TelegramChatID},
		"text":    {message},
	})
	return err
}

func (s *server) testTelegramSettings(w http.ResponseWriter, r *http.Request) {
	settings := s.store.snapshot().Settings
	if settings == nil || settings.TelegramBotToken == "" || settings.TelegramChatID == "" {
		writeError(w, http.StatusBadRequest, "Сначала сохраните токен бота и Chat ID")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	bot, err := telegramCall(ctx, settings, "getMe", url.Values{})
	if err == nil {
		err = sendTelegramMessage(ctx, settings, systemName(settings)+": тестовое уведомление доставлено.")
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "Не удалось связаться с Telegram: "+err.Error())
		return
	}
	username := bot.Result.Username
	_ = s.store.update(func(state *model.State) error {
		state.Settings.TelegramBotUsername = username
		return nil
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "botUsername": username})
}

func (s *server) notify(message string) {
	s.notifyCategory("system", message)
}

func (s *server) notifyCategory(category, message string) {
	s.sendPWANotifications(category, message)
	settings := s.store.snapshot().Settings
	if settings == nil || !settings.TelegramEnabled || settings.TelegramBotToken == "" || settings.TelegramChatID == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = sendTelegramMessage(ctx, settings, systemName(settings)+"\n"+message)
	}()
}

func (s *server) monitorOfflineNodes(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			state := s.store.snapshot()
			var offlineIDs []string
			for _, node := range state.Nodes {
				if !node.LastSeenAt.IsZero() && !nodeOnline(node, now) && !node.OfflineNotified {
					offlineIDs = append(offlineIDs, node.ID)
				}
			}
			if len(offlineIDs) == 0 {
				continue
			}
			var messages []string
			_ = s.store.update(func(current *model.State) error {
				for _, id := range offlineIDs {
					node := findNode(current, id)
					if node == nil || node.OfflineNotified || nodeOnline(node, now) {
						continue
					}
					node.OfflineNotified = true
					current.Events = append(current.Events, newEvent("node.offline", "Нода потеряла связь", node.ID, nil))
					messages = append(messages, "Нода потеряла связь: "+node.Name)
				}
				return nil
			})
			for _, message := range messages {
				s.notifyCategory("nodes", message)
			}
		}
	}
}
