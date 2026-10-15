package service

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *AccountTestService) testWorkBuddyConnection(c *gin.Context, account *Account, modelID, prompt string) error {
	if s.WorkBuddy() == nil {
		return s.sendErrorAndEnd(c, "WorkBuddy service is unavailable")
	}
	if modelID == "" {
		catalog, err := s.WorkBuddy().Models(c.Request.Context(), account)
		if err != nil {
			return s.sendErrorAndEnd(c, err.Error())
		}
		modelID = catalog.Models[0]
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = "Say exactly: PONG"
	}
	body, _ := json.Marshal(map[string]any{"model": account.GetMappedModel(modelID), "messages": []any{map[string]any{"role": "user", "content": prompt}}, "max_tokens": 64})
	s.sendEvent(c, TestEvent{Type: "test_start", Model: modelID})
	resp, err := s.WorkBuddy().Chat(c.Request.Context(), c, account, body, false, s.cfg.JWT.Secret)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	defer closeWorkBuddyBody(resp.Body)
	if resp.StatusCode >= 400 {
		return s.sendErrorAndEnd(c, "WorkBuddy rejected the request ("+resp.Status+")")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, workBuddyBodyLimit+1))
	if err != nil || len(data) > workBuddyBodyLimit {
		return s.sendErrorAndEnd(c, "Failed to read WorkBuddy response")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Choices) == 0 {
		return s.sendErrorAndEnd(c, "Invalid WorkBuddy response")
	}
	s.sendEvent(c, TestEvent{Type: "content", Text: result.Choices[0].Message.Content})
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true, Data: result.Usage})
	return nil
}
