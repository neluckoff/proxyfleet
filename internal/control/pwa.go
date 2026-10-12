package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/neluckoff/proxyfleet/internal/model"
)

type pushSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Preferences struct {
		Nodes   bool `json:"nodes"`
		Proxies bool `json:"proxies"`
		System  bool `json:"system"`
	} `json:"preferences"`
}

func (s *server) pwaInfo(w http.ResponseWriter, r *http.Request) {
	if err := s.ensurePWASecrets(); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось подготовить PWA-уведомления")
		return
	}
	state := s.store.snapshot()
	result := map[string]any{
		"publicKey":         state.Settings.VAPIDPublicKey,
		"subscriptionCount": len(state.PushSubscriptions),
	}
	endpoint := strings.TrimSpace(r.URL.Query().Get("endpoint"))
	for _, subscription := range state.PushSubscriptions {
		if endpoint != "" && subscription.Endpoint == endpoint {
			result["subscription"] = publicPushSubscription(subscription)
			break
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) subscribePWA(w http.ResponseWriter, r *http.Request) {
	var input pushSubscriptionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if !validPushEndpoint(input.Endpoint) || input.Keys.P256DH == "" || input.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "Браузер передал некорректную push-подписку")
		return
	}
	now := time.Now().UTC()
	var result *model.PushSubscription
	err := s.store.update(func(state *model.State) error {
		for _, subscription := range state.PushSubscriptions {
			if subscription.Endpoint != input.Endpoint {
				continue
			}
			subscription.P256DH = input.Keys.P256DH
			subscription.Auth = input.Keys.Auth
			subscription.Nodes = input.Preferences.Nodes
			subscription.Proxies = input.Preferences.Proxies
			subscription.System = input.Preferences.System
			subscription.UpdatedAt = now
			result = subscription
			return nil
		}
		result = &model.PushSubscription{
			ID: randomHex(8), Endpoint: input.Endpoint, P256DH: input.Keys.P256DH, Auth: input.Keys.Auth,
			Nodes: input.Preferences.Nodes, Proxies: input.Preferences.Proxies, System: input.Preferences.System,
			CreatedAt: now, UpdatedAt: now,
		}
		state.PushSubscriptions = append(state.PushSubscriptions, result)
		state.Events = append(state.Events, newEvent("pwa.subscribed", "Включены PWA-уведомления", "", nil))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить push-подписку")
		return
	}
	writeJSON(w, http.StatusOK, publicPushSubscription(result))
}

func (s *server) unsubscribePWA(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	removed := false
	err := s.store.update(func(state *model.State) error {
		state.PushSubscriptions = slices.DeleteFunc(state.PushSubscriptions, func(subscription *model.PushSubscription) bool {
			if subscription.Endpoint == input.Endpoint {
				removed = true
				return true
			}
			return false
		})
		if removed {
			state.Events = append(state.Events, newEvent("pwa.unsubscribed", "PWA-уведомления выключены", "", nil))
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить push-подписку")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) testPWA(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Endpoint string `json:"endpoint"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	state := s.store.snapshot()
	var subscription *model.PushSubscription
	for _, candidate := range state.PushSubscriptions {
		if candidate.Endpoint == input.Endpoint {
			subscription = candidate
			break
		}
	}
	if subscription == nil {
		writeError(w, http.StatusNotFound, "Push-подписка этого браузера не найдена")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := sendWebPush(ctx, state.Settings, subscription, pushMessage{
		Title: systemName(state.Settings), Body: "Тестовое PWA-уведомление доставлено.", Tag: "proxyfleet-test", URL: "/settings",
	}); err != nil {
		writeError(w, http.StatusBadGateway, "Не удалось отправить push-уведомление: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) ensurePWASecrets() error {
	state := s.store.snapshot()
	if state.Settings == nil {
		return errors.New("settings are missing")
	}
	if state.Settings.VAPIDPublicKey != "" && state.Settings.VAPIDPrivateKey != "" && state.Settings.DataEncryptionKey != "" {
		return nil
	}
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	return s.store.update(func(current *model.State) error {
		if current.Settings == nil {
			return errors.New("settings are missing")
		}
		if current.Settings.DataEncryptionKey == "" {
			current.Settings.DataEncryptionKey = randomToken(32)
		}
		if current.Settings.VAPIDPublicKey == "" || current.Settings.VAPIDPrivateKey == "" {
			current.Settings.VAPIDPrivateKey = privateKey
			current.Settings.VAPIDPublicKey = publicKey
		}
		return nil
	})
}

type pushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag,omitempty"`
	URL   string `json:"url,omitempty"`
}

func (s *server) sendPWANotifications(category, message string) {
	state := s.store.snapshot()
	if state.Settings == nil || state.Settings.VAPIDPublicKey == "" || state.Settings.VAPIDPrivateKey == "" {
		return
	}
	path := "/events"
	if category == "nodes" {
		path = "/nodes"
	} else if category == "proxies" {
		path = "/proxies"
	}
	for _, subscription := range state.PushSubscriptions {
		if !pushCategoryEnabled(subscription, category) {
			continue
		}
		subscription := subscription
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := sendWebPush(ctx, state.Settings, subscription, pushMessage{
				Title: systemName(state.Settings), Body: message, Tag: "proxyfleet-" + category, URL: path,
			})
			if errors.Is(err, errPushSubscriptionExpired) {
				_ = s.removePushSubscription(subscription.Endpoint)
			}
		}()
	}
}

var errPushSubscriptionExpired = errors.New("push subscription expired")

func sendWebPush(ctx context.Context, settings *model.Settings, subscription *model.PushSubscription, message pushMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	subscriber := strings.TrimSpace(settings.PublicURL)
	if subscriber == "" {
		subscriber = "mailto:admin@localhost"
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, &webpush.Options{
		Subscriber: subscriber, VAPIDPublicKey: settings.VAPIDPublicKey, VAPIDPrivateKey: settings.VAPIDPrivateKey,
		TTL: 300, Topic: message.Tag,
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusGone || response.StatusCode == http.StatusNotFound {
		return errPushSubscriptionExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("push service returned %s", response.Status)
	}
	return nil
}

func (s *server) removePushSubscription(endpoint string) error {
	return s.store.update(func(state *model.State) error {
		state.PushSubscriptions = slices.DeleteFunc(state.PushSubscriptions, func(subscription *model.PushSubscription) bool {
			return subscription.Endpoint == endpoint
		})
		return nil
	})
}

func validPushEndpoint(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func pushCategoryEnabled(subscription *model.PushSubscription, category string) bool {
	switch category {
	case "nodes":
		return subscription.Nodes
	case "proxies":
		return subscription.Proxies
	default:
		return subscription.System
	}
}

func publicPushSubscription(subscription *model.PushSubscription) map[string]any {
	if subscription == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id": subscription.ID,
		"preferences": map[string]bool{
			"nodes": subscription.Nodes, "proxies": subscription.Proxies, "system": subscription.System,
		},
	}
}
