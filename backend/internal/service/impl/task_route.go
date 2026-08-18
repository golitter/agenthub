package impl

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

const (
	routeModeDirect       = "direct"
	routeModeOrchestrator = "orchestrator"
	routeModeUnchanged    = "unchanged"
)

type routeAgent struct {
	RouteID      string
	MentionLabel string
	Aliases      []string
	SessionID    string
	AgentType    string
	AgentName    string
}

type routeMention struct {
	Kind  string
	Agent routeAgent
	Raw   string
}

var agentTypeAliases = map[string][]string{
	"claude-code":  {"Claude Code"},
	"opencode":     {"OpenCode"},
	"codex":        {"Codex"},
	"pi":           {"Pi"},
	"orchestrator": {"Orchestrator"},
}

func resolveMessageRoute(req service.RunTaskInput, sessions []model.Session) (service.MessageRoute, error) {
	agentType := requestedAgentType(req, sessions)
	route := service.MessageRoute{
		Mode:           routeModeUnchanged,
		SessionID:      req.SessionID,
		AgentType:      agentType,
		AgentMessage:   req.Message,
		DisplayMessage: req.Message,
	}

	if req.SkipUserMessage {
		if sessionModel, ok := findSessionByID(sessions, req.SessionID); ok {
			route.AgentName = sessionModel.AgentName
			route.RouteID = buildRouteAgentForSession(sessions, sessionModel.SessionID).RouteID
		}
		return route, nil
	}

	agents := buildRouteAgents(sessions)
	orchestrator, hasOrchestrator := findRouteAgentByType(agents, "orchestrator")
	mentions, remaining, hasMention, err := parseLeadingMentions(req.Message, agents)
	if err != nil {
		return route, err
	}

	if hasMention {
		if len(mentions) == 1 && mentions[0].Kind == "agent" && mentions[0].Agent.AgentType != "orchestrator" {
			target := mentions[0].Agent
			route.Mode = routeModeDirect
			route.SessionID = target.SessionID
			route.AgentType = target.AgentType
			route.AgentName = target.AgentName
			route.RouteID = target.RouteID
			route.AgentMessage = nonEmptyMessage(remaining, req.Message)
			return route, nil
		}
		if !hasOrchestrator {
			return route, fmt.Errorf("orchestrator session not found for multi-agent route")
		}
		route.Mode = routeModeOrchestrator
		route.SessionID = orchestrator.SessionID
		route.AgentType = orchestrator.AgentType
		route.AgentName = orchestrator.AgentName
		route.RouteID = orchestrator.RouteID
		if len(mentions) == 1 && mentions[0].Agent.AgentType == "orchestrator" {
			route.AgentMessage = nonEmptyMessage(remaining, req.Message)
		}
		return route, nil
	}

	if hasOrchestrator && len(sessions) > 1 {
		route.Mode = routeModeOrchestrator
		route.SessionID = orchestrator.SessionID
		route.AgentType = orchestrator.AgentType
		route.AgentName = orchestrator.AgentName
		route.RouteID = orchestrator.RouteID
		return route, nil
	}

	if sessionModel, ok := findSessionByID(sessions, req.SessionID); ok {
		route.AgentName = sessionModel.AgentName
		route.RouteID = buildRouteAgentForSession(sessions, sessionModel.SessionID).RouteID
	}
	return route, nil
}

func requestedAgentType(req service.RunTaskInput, sessions []model.Session) string {
	if req.AgentType != "" {
		return req.AgentType
	}
	if sessionModel, ok := findSessionByID(sessions, req.SessionID); ok && sessionModel.AgentType != "" {
		return sessionModel.AgentType
	}
	return "claude-code"
}

func nonEmptyMessage(message, fallback string) string {
	if strings.TrimSpace(message) == "" {
		return fallback
	}
	return strings.TrimSpace(message)
}

func buildRouteAgents(sessions []model.Session) []routeAgent {
	seenBase := make(map[string]int, len(sessions))
	agents := make([]routeAgent, 0, len(sessions))
	for _, sessionModel := range sessions {
		base := strings.TrimSpace(sessionModel.AgentName)
		if base == "" {
			base = strings.TrimSpace(sessionModel.AgentType)
		}
		if base == "" {
			base = sessionModel.SessionID
		}
		seenBase[base]++
		routeID := base
		if seenBase[base] > 1 {
			routeID = fmt.Sprintf("%s-%d", base, seenBase[base])
		}
		agents = append(agents, routeAgent{
			RouteID:      routeID,
			MentionLabel: routeID,
			Aliases:      uniqueNonEmpty(append([]string{routeID, sessionModel.AgentName, sessionModel.AgentType}, agentTypeAliases[sessionModel.AgentType]...)...),
			SessionID:    sessionModel.SessionID,
			AgentType:    sessionModel.AgentType,
			AgentName:    sessionModel.AgentName,
		})
	}
	return agents
}

func buildRouteAgentForSession(sessions []model.Session, sessionID string) routeAgent {
	for _, agent := range buildRouteAgents(sessions) {
		if agent.SessionID == sessionID {
			return agent
		}
	}
	return routeAgent{}
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func findRouteAgentByType(agents []routeAgent, agentType string) (routeAgent, bool) {
	for _, agent := range agents {
		if agent.AgentType == agentType {
			return agent, true
		}
	}
	return routeAgent{}, false
}

func findSessionByID(sessions []model.Session, sessionID string) (model.Session, bool) {
	for _, sessionModel := range sessions {
		if sessionModel.SessionID == sessionID {
			return sessionModel, true
		}
	}
	return model.Session{}, false
}

func parseLeadingMentions(message string, agents []routeAgent) ([]routeMention, string, bool, error) {
	remaining := strings.TrimLeftFunc(message, unicode.IsSpace)
	if !strings.HasPrefix(remaining, "@") {
		return nil, message, false, nil
	}

	mentions := make([]routeMention, 0, 2)
	for strings.HasPrefix(remaining, "@") {
		mention, consumed, err := matchMention(remaining[1:], agents)
		if err != nil {
			return nil, message, true, err
		}
		mentions = append(mentions, mention)
		remaining = strings.TrimLeftFunc(remaining[1+consumed:], unicode.IsSpace)
	}
	return mentions, remaining, true, nil
}

func matchMention(rest string, agents []routeAgent) (routeMention, int, error) {
	if consumed, ok := matchLiteralMention(rest, "all"); ok {
		return routeMention{Kind: "all", Raw: "all"}, consumed, nil
	}

	if agent, consumed, ok := matchRouteIDMention(rest, agents); ok {
		return routeMention{Kind: "agent", Agent: agent, Raw: agent.RouteID}, consumed, nil
	}

	matches := make([]routeAgent, 0, 2)
	consumed := 0
	for _, agent := range agents {
		for _, alias := range agent.Aliases {
			if alias == agent.RouteID {
				continue
			}
			if n, ok := matchLiteralMention(rest, alias); ok {
				if n > consumed {
					matches = matches[:0]
					consumed = n
				}
				if n == consumed {
					matches = append(matches, agent)
				}
			}
		}
	}
	if len(matches) == 1 {
		return routeMention{Kind: "agent", Agent: matches[0], Raw: rest[:consumed]}, consumed, nil
	}
	if len(matches) > 1 {
		return routeMention{}, 0, fmt.Errorf("mention %q matches multiple agents", rest[:consumed])
	}

	raw := readMentionToken(rest)
	if raw == "" {
		raw = "@"
	}
	return routeMention{}, 0, fmt.Errorf("unknown agent mention: @%s", raw)
}

func matchRouteIDMention(rest string, agents []routeAgent) (routeAgent, int, bool) {
	var matched routeAgent
	consumed := 0
	found := false
	for _, agent := range agents {
		if n, ok := matchLiteralMention(rest, agent.RouteID); ok && n > consumed {
			matched = agent
			consumed = n
			found = true
		}
	}
	return matched, consumed, found
}

func matchLiteralMention(rest, literal string) (int, bool) {
	if literal == "" || len(rest) < len(literal) {
		return 0, false
	}
	if !strings.EqualFold(rest[:len(literal)], literal) {
		return 0, false
	}
	if len(rest) == len(literal) {
		return len(literal), true
	}
	next, _ := utf8.DecodeRuneInString(rest[len(literal):])
	if unicode.IsSpace(next) {
		return len(literal), true
	}
	return 0, false
}

func readMentionToken(rest string) string {
	for i, r := range rest {
		if unicode.IsSpace(r) || r == '@' {
			return rest[:i]
		}
	}
	return rest
}
