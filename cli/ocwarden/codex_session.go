package main

// codex_session.go is the Codex counterpart to Claude's direct TUI launch.
// A small ocwarden sidecar owns one stdio Codex App Server, performs the
// initialize/thread/turn handshake, starts ocagent listen after the boot turn,
// and translates listener output into turn/start or turn/steer calls. This
// keeps lifecycle and SSE ownership identical to the existing Claude design
// without requiring a human to attach to a TUI.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func buildCodexLaunchCommand(wardenBin, codexBin, workdir, personaFile, tokenFile,
	agentID, base, session, socket, model, effort string, extraEnv [][2]string,
	envRendered string) string {
	cd := "cd " + shellQuote(workdir) + "; "
	if envRendered != "" {
		cd += "[ -f " + shellQuote(envRendered) + " ] && . " + shellQuote(envRendered) + "; "
	}
	pairs := [][2]string{
		{"OC_BASE", base},
		{"OC_ID", agentID},
		{"OC_SESSION", session},
		{"OC_TMUX_SOCKET", socket},
	}
	pairs = append(pairs, extraEnv...)
	kvs := []string{`OC_TOKEN="$(/bin/cat ` + shellQuote(tokenFile) + `)"`}
	for _, pair := range pairs {
		kvs = append(kvs, pair[0]+"="+shellQuote(pair[1]))
	}
	exports := "export " + strings.Join(kvs, " ") + "; "
	exports += "export PATH=" + shellQuote(workdir) + `:"$PATH"; `
	parts := []string{
		shellQuote(wardenBin), "codex-session",
		"--codex-bin", shellQuote(codexBin),
		"--workdir", shellQuote(workdir),
		"--persona", shellQuote(personaFile),
		"--agent-id", shellQuote(agentID),
		"--model", shellQuote(model),
		"--effort", shellQuote(normalizeCodexEffort(effort)),
	}
	return cd + exports + "exec " + strings.Join(parts, " ")
}

func normalizeCodexEffort(effort string) string {
	switch strings.TrimSpace(effort) {
	case "low", "high":
		return strings.TrimSpace(effort)
	default:
		return "medium"
	}
}

func codexPersonaInstruction(personaFile, model string) string {
	instruction := "Read " + personaFile +
		" completely before acting. It is your OffiCraft identity and operating context. " +
		"Never use request_user_input for normal questions; create an OffiCraft reply card instead. "
	if strings.TrimSpace(model) == "" {
		return instruction +
			"The OffiCraft launch model setting is blank, so the machine's Codex default applies. " +
			"When calling report_waking, omit its optional model argument; never guess or persist a model name."
	}
	return instruction + "The explicit OffiCraft launch model is " + model +
		"; pass that exact value as report_waking's model argument."
}

type appServerMessage map[string]any

type codexSession struct {
	in          io.WriteCloser
	messages    <-chan appServerMessage
	writeMu     sync.Mutex
	nextID      int
	threadID    string
	turnID      string
	active      bool
	base        string
	token       string
	workdir     string
	model       string
	effort      string
	compactions int
	// completedCompactions makes the App Server's item/completed stream
	// idempotent. Replayed notifications must not look like fresh context
	// compactions and accidentally recycle a just-booted agent.
	completedCompactions map[string]struct{}
}

func (s *codexSession) send(method string, params map[string]any) int {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.nextID++
	id := s.nextID
	_ = json.NewEncoder(s.in).Encode(appServerMessage{
		"id": id, "method": method, "params": params,
	})
	return id
}

func (s *codexSession) notify(method string, params map[string]any) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = json.NewEncoder(s.in).Encode(appServerMessage{"method": method, "params": params})
}

func messageID(msg appServerMessage) int {
	switch value := msg["id"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func nestedString(obj map[string]any, keys ...string) string {
	var current any = obj
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	text, _ := current.(string)
	return text
}

func (s *codexSession) waitResponse(id int) (appServerMessage, error) {
	for msg := range s.messages {
		if messageID(msg) != id {
			continue
		}
		if problem, ok := msg["error"].(map[string]any); ok {
			return nil, fmt.Errorf("app-server request failed: %v", problem["message"])
		}
		return msg, nil
	}
	return nil, errors.New("app-server exited before responding")
}

func (s *codexSession) startTurn(text string) {
	params := map[string]any{
		"threadId": s.threadID,
		"input":    []any{map[string]any{"type": "text", "text": text}},
		"effort":   s.effort,
	}
	s.send("turn/start", params)
}

func (s *codexSession) steerOrStart(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if s.active && s.turnID != "" {
		s.send("turn/steer", map[string]any{
			"threadId": s.threadID, "expectedTurnId": s.turnID,
			"input": []any{map[string]any{"type": "text", "text": text}},
		})
		return
	}
	s.startTurn(text)
}

func codexAppReader(r io.Reader) <-chan appServerMessage {
	out := make(chan appServerMessage, 64)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 8<<20)
		for scanner.Scan() {
			var msg appServerMessage
			if json.Unmarshal(scanner.Bytes(), &msg) == nil {
				out <- msg
			}
		}
	}()
	return out
}

func (s *codexSession) openReplyCard(question map[string]any, bind string) string {
	header, _ := question["header"].(string)
	body, _ := question["question"].(string)
	secret, _ := question["isSecret"].(bool)
	kind := "decision"
	options := []string{}
	if secret {
		kind = "action"
		options = []string{"已完成（不要在卡片中貼秘密）"}
		body += "\n\n這是秘密資料請求；請只完成所需動作，不要把秘密貼進卡片。"
	} else if raw, ok := question["options"].([]any); ok {
		for _, item := range raw {
			if option, ok := item.(map[string]any); ok {
				if label, ok := option["label"].(string); ok && strings.TrimSpace(label) != "" {
					options = append(options, label)
				}
			}
		}
	}
	if len(options) == 0 {
		options = []string{"請在文字回覆中回答"}
	}
	if len(options) > 4 {
		options = options[:4]
	}
	if strings.TrimSpace(header) == "" {
		header = body
	}
	if strings.TrimSpace(header) == "" {
		header = "Codex 需要你的回覆"
	}
	payload := map[string]any{
		"kind": kind, "summary": header, "body": body,
		"options": options, "bind": bind,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.base, "/")+
		"/api/reply-cards", bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
	id, _ := result["id"].(string)
	return id
}

func jsonNumber(value any) float64 {
	number, _ := value.(float64)
	return number
}

func (s *codexSession) post(path string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.base, "/")+path,
		bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func (s *codexSession) reportTokenUsage(params map[string]any) {
	usage, _ := params["tokenUsage"].(map[string]any)
	total, _ := usage["total"].(map[string]any)
	last, _ := usage["last"].(map[string]any)
	window := jsonNumber(usage["modelContextWindow"])
	// "total" is cumulative across the thread and can exceed one context
	// window after a few turns. "last" is the current turn's context gauge.
	used := jsonNumber(last["totalTokens"])
	if window > 0 {
		s.post("/api/agent/context", map[string]any{
			"context_pct":      used / window * 100,
			"compaction_count": s.compactions,
		})
	}
	tokens := map[string]any{}
	for _, key := range []string{
		"inputTokens", "cachedInputTokens", "outputTokens",
		"reasoningOutputTokens", "totalTokens",
	} {
		if value, ok := total[key]; ok {
			tokens[key] = value
		}
	}
	s.post("/api/monitoring/telemetry", map[string]any{
		"runtime": "codex", "tokens": tokens, "effort": s.effort,
	})
}

// recordCompaction consumes the current App Server signal. Context compaction is
// an item, not a turn: counting the completed item avoids guessing from token
// percentages and intentionally ignores the deprecated thread/compacted echo.
func (s *codexSession) recordCompaction(params map[string]any) {
	item, _ := params["item"].(map[string]any)
	if item == nil || item["type"] != "contextCompaction" {
		return
	}
	id, _ := item["id"].(string)
	if id == "" {
		return // completion events are item-addressed; never count an anonymous echo
	}
	if s.completedCompactions == nil {
		s.completedCompactions = make(map[string]struct{})
	}
	if _, seen := s.completedCompactions[id]; seen {
		return
	}
	s.completedCompactions[id] = struct{}{}
	s.compactions++
}

func (s *codexSession) handleServerRequest(msg appServerMessage) {
	method, _ := msg["method"].(string)
	// App Server RequestId is string | int64. Echo the exact JSON value back;
	// coercing a string id to zero would leave Codex waiting forever.
	id := msg["id"]
	params, _ := msg["params"].(map[string]any)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	enc := json.NewEncoder(s.in)
	switch method {
	case "item/tool/requestUserInput":
		answers := map[string]any{}
		questions, _ := params["questions"].([]any)
		for index, raw := range questions {
			question, _ := raw.(map[string]any)
			bind := "none"
			if index == 0 {
				bind = ""
			}
			cardID := s.openReplyCard(question, bind)
			qid, _ := question["id"].(string)
			message := "Deferred to OffiCraft; end this turn and wait for the reply-card event."
			if cardID != "" {
				message = "Deferred to OffiCraft reply card " + cardID +
					"; end this turn and wait for its SSE answer event."
			} else {
				message = "OffiCraft reply-card creation failed; do not wait for terminal input. " +
					"Continue if safe or report the failure through OffiCraft chat."
			}
			answers[qid] = map[string]any{"answers": []string{message}}
		}
		_ = enc.Encode(appServerMessage{"id": id, "result": map[string]any{"answers": answers}})
	case "mcpServer/elicitation/request":
		_ = enc.Encode(appServerMessage{"id": id, "result": map[string]any{"action": "decline"}})
	default:
		_ = enc.Encode(appServerMessage{"id": id, "error": map[string]any{
			"code": -32601, "message": "OffiCraft sidecar does not support this server request",
		}})
	}
}

func actionableCodexListenerLine(line string) bool {
	// Transport diagnostics belong in the pane, not in the model transcript.
	// Sending the connected/reconnect chatter creates empty, token-heavy turns.
	return !strings.HasPrefix(strings.TrimSpace(line), "[ocagent] listen:")
}

func runCodexSession(argv []string, env func(string) string, out io.Writer) int {
	fs := flag.NewFlagSet("ocwarden codex-session", flag.ContinueOnError)
	fs.SetOutput(out)
	codexBin := fs.String("codex-bin", "", "")
	workdir := fs.String("workdir", "", "")
	persona := fs.String("persona", "", "")
	agentID := fs.String("agent-id", "", "")
	model := fs.String("model", "", "")
	effort := fs.String("effort", "medium", "")
	if fs.Parse(argv) != nil || *codexBin == "" || *workdir == "" || *persona == "" {
		fmt.Fprintln(out, "codex-session: missing required launch parameters")
		return 2
	}
	cmd := exec.Command(*codexBin, "app-server")
	cmd.Dir = *workdir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(out, "codex-session: stdin: %v\n", err)
		return 1
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(out, "codex-session: stdout: %v\n", err)
		return 1
	}
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(out, "codex-session: start app-server: %v\n", err)
		return 1
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	s := &codexSession{
		in: stdin, messages: codexAppReader(stdout), nextID: 0,
		base: env("OC_BASE"), token: env("OC_TOKEN"), workdir: *workdir,
		model: *model, effort: normalizeCodexEffort(*effort),
	}
	initializeID := s.send("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name": "officraft", "title": "OffiCraft", "version": "0.1.0",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if _, err := s.waitResponse(initializeID); err != nil {
		fmt.Fprintf(out, "codex-session: initialize: %v\n", err)
		return 1
	}
	s.notify("initialized", map[string]any{})
	threadParams := map[string]any{
		"cwd": *workdir, "approvalPolicy": "never", "sandbox": "danger-full-access",
		"developerInstructions": codexPersonaInstruction(*persona, *model),
		"config": map[string]any{
			"features": map[string]any{"default_mode_request_user_input": false},
			"mcp_servers": map[string]any{"officraft": map[string]any{
				"url":          strings.TrimRight(s.base, "/") + "/api/mcp",
				"http_headers": map[string]any{"Authorization": "Bearer " + s.token},
			}},
		},
	}
	if *model != "" {
		threadParams["model"] = *model
	}
	threadID := s.send("thread/start", threadParams)
	threadResp, err := s.waitResponse(threadID)
	if err != nil {
		fmt.Fprintf(out, "codex-session: thread/start: %v\n", err)
		return 1
	}
	s.threadID = nestedString(threadResp, "result", "thread", "id")
	if s.threadID == "" {
		fmt.Fprintln(out, "codex-session: thread/start returned no thread id")
		return 1
	}
	s.startTurn("開始。")

	listenerLines := make(chan string, 32)
	listenerStarted := false
	var listenerCmd *exec.Cmd
	defer func() {
		if listenerCmd != nil && listenerCmd.Process != nil {
			_ = listenerCmd.Process.Kill()
		}
	}()
	for s.messages != nil {
		select {
		case line, ok := <-listenerLines:
			if !ok {
				fmt.Fprintln(out, "codex-session: ocagent listen exited; ending session for reconciliation")
				return 1
			}
			if actionableCodexListenerLine(line) {
				s.steerOrStart(line)
			}
		case msg, ok := <-s.messages:
			if !ok {
				s.messages = nil
				continue
			}
			if _, hasID := msg["id"]; hasID {
				if _, hasMethod := msg["method"]; hasMethod {
					s.handleServerRequest(msg)
				}
				continue
			}
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			switch method {
			case "turn/started":
				s.active = true
				s.turnID = nestedString(params, "turn", "id")
			case "turn/completed":
				s.active = false
				s.turnID = ""
				if !listenerStarted {
					listenerStarted = true
					listenerCmd = exec.Command(filepath.Join(*workdir, "ocagent"), "listen")
					listenerCmd.Dir = *workdir
					listenerCmd.Stderr = out
					pipe, pipeErr := listenerCmd.StdoutPipe()
					if pipeErr != nil {
						fmt.Fprintf(out, "codex-session: ocagent listen stdout: %v\n", pipeErr)
						return 1
					}
					if startErr := listenerCmd.Start(); startErr != nil {
						fmt.Fprintf(out, "codex-session: start ocagent listen: %v\n", startErr)
						return 1
					}
					go func(listener *exec.Cmd) {
						scanner := bufio.NewScanner(pipe)
						scanner.Buffer(make([]byte, 64*1024), 8<<20)
						for scanner.Scan() {
							listenerLines <- scanner.Text()
						}
						_ = listener.Wait()
						close(listenerLines)
					}(listenerCmd)
				}
			case "thread/tokenUsage/updated":
				s.reportTokenUsage(params)
			case "item/completed":
				s.recordCompaction(params)
			case "item/tool/requestUserInput", "mcpServer/elicitation/request":
				s.handleServerRequest(msg)
			}
		}
	}
	_ = agentID // retained in argv for diagnostics and future thread metadata
	return 1
}
