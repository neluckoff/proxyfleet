package control

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func (s *server) listUsers(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	result := make([]map[string]any, 0, len(state.Users))
	for _, user := range state.Users {
		result = append(result, publicUser(user))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string   `json:"name"`
		Username string   `json:"username"`
		Password string   `json:"password"`
		NodeIDs  []string `json:"nodeIds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	if input.Name == "" || input.Username == "" {
		writeError(w, http.StatusBadRequest, "Укажите название и логин")
		return
	}
	if input.Password == "" {
		input.Password = randomToken(18)
	}
	if len(input.Password) < 12 {
		writeError(w, http.StatusBadRequest, "Пароль должен содержать не менее 12 символов")
		return
	}
	user := &model.ProxyUser{ID: randomHex(8), Name: input.Name, Username: input.Username, PasswordHash: credentialHash(input.Username, input.Password), NodeIDs: uniqueStrings(input.NodeIDs), Enabled: true, CreatedAt: time.Now().UTC()}
	err := s.store.update(func(state *model.State) error {
		for _, existing := range state.Users {
			if strings.EqualFold(existing.Username, user.Username) {
				return errors.New("login exists")
			}
		}
		if !nodeIDsExist(state, user.NodeIDs) {
			return errors.New("unknown node")
		}
		state.Users = append(state.Users, user)
		bumpNodes(state, user.NodeIDs)
		state.Events = append(state.Events, newEvent("user.created", "Создан пользователь прокси", "", map[string]any{"username": user.Username}))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось создать пользователя: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": publicUser(user), "password": input.Password})
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name     *string   `json:"name"`
		Password *string   `json:"password"`
		NodeIDs  *[]string `json:"nodeIds"`
		Enabled  *bool     `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var result *model.ProxyUser
	var revealedPassword string
	err := s.store.update(func(state *model.State) error {
		for _, user := range state.Users {
			if user.ID != id {
				continue
			}
			oldNodeIDs := append([]string(nil), user.NodeIDs...)
			if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
				user.Name = strings.TrimSpace(*input.Name)
			}
			if input.Enabled != nil {
				user.Enabled = *input.Enabled
			}
			if input.NodeIDs != nil {
				nodes := uniqueStrings(*input.NodeIDs)
				if !nodeIDsExist(state, nodes) {
					return errors.New("unknown node")
				}
				user.NodeIDs = nodes
			}
			if input.Password != nil {
				revealedPassword = *input.Password
				if revealedPassword == "" {
					revealedPassword = randomToken(18)
				}
				if len(revealedPassword) < 12 {
					return errors.New("password too short")
				}
				user.PasswordHash = credentialHash(user.Username, revealedPassword)
			}
			bumpNodes(state, uniqueStrings(append(oldNodeIDs, user.NodeIDs...)))
			result = user
			state.Events = append(state.Events, newEvent("user.updated", "Пользователь прокси обновлён", "", map[string]any{"username": user.Username}))
			return nil
		}
		return os.ErrNotExist
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось обновить пользователя: "+err.Error())
		return
	}
	response := map[string]any{"user": publicUser(result)}
	if revealedPassword != "" {
		response["password"] = revealedPassword
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.update(func(state *model.State) error {
		var nodeIDs []string
		before := len(state.Users)
		state.Users = slices.DeleteFunc(state.Users, func(user *model.ProxyUser) bool {
			if user.ID == id {
				nodeIDs = user.NodeIDs
				return true
			}
			return false
		})
		if len(state.Users) == before {
			return os.ErrNotExist
		}
		bumpNodes(state, nodeIDs)
		state.Events = append(state.Events, newEvent("user.deleted", "Пользователь прокси удалён", "", nil))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить пользователя")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func publicUser(user *model.ProxyUser) map[string]any {
	return map[string]any{"id": user.ID, "name": user.Name, "username": user.Username, "nodeIds": user.NodeIDs, "enabled": user.Enabled, "createdAt": user.CreatedAt}
}

func nodeIDsExist(state *model.State, ids []string) bool {
	for _, id := range ids {
		if findNode(state, id) == nil {
			return false
		}
	}
	return true
}

func bumpNodes(state *model.State, ids []string) {
	for _, id := range ids {
		if node := findNode(state, id); node != nil {
			node.ConfigVersion++
		}
	}
}
