package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Provider string

const (
	ProviderOllama Provider = "Ollama"
	ProviderGemini Provider = "Gemini"
)

type Suggestion struct {
	Summary       string
	Description   string
	CommitMessage string
}

type OllamaModel struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type ollamaTags struct {
	Models []OllamaModel `json:"models"`
}

func ListOllamaModels(ctx context.Context) ([]OllamaModel, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:11434/api/tags", nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("ollama returned %s", resp.Status)
	}
	var tags ollamaTags
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, err
	}
	return tags.Models, nil
}

func Generate(ctx context.Context, provider Provider, model, apiKey, projectPath string) (Suggestion, error) {
	contextText, err := BuildProjectContext(projectPath)
	if err != nil {
		return Suggestion{}, err
	}
	prompt := `You are helping archive a software project safely into Git.
Read the project metadata and selected text files below. Do not invent features.
Return EXACTLY this format:
SUMMARY: <2-4 concise sentences describing what the project does>
DESCRIPTION: <one GitHub repository description, max 120 characters>
COMMIT: <one conventional-style archive/backup commit message, max 72 characters>

PROJECT CONTEXT:
` + contextText

	var text string
	switch provider {
	case ProviderOllama:
		text, err = generateOllama(ctx, model, prompt)
	case ProviderGemini:
		text, err = generateGemini(ctx, model, apiKey, prompt)
	default:
		err = errors.New("unknown AI provider")
	}
	if err != nil {
		return Suggestion{}, err
	}
	return parseSuggestion(text), nil
}

func BuildProjectContext(root string) (string, error) {
	var b strings.Builder
	b.WriteString("Project: " + filepath.Base(root) + "\n")

	files := []string{"package.json", "README.md", "README", "vite.config.ts", "vite.config.js", "next.config.js", "next.config.mjs", "next.config.ts", "tsconfig.json"}
	for _, name := range files {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > 12000 {
			data = data[:12000]
		}
		b.WriteString("\n--- " + name + " ---\n")
		b.WriteString(redactSensitiveText(string(data)))
		b.WriteByte('\n')
	}

	// Add only filenames from source-like directories. We intentionally avoid
	// reading .env files, credentials, node_modules, build output, or arbitrary
	// source bodies into a remote model.
	var names []string
	for _, dir := range []string{"src", "app", "pages", "components", "server", "api"} {
		base := filepath.Join(root, dir)
		_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				if info.Name() == "node_modules" || info.Name() == ".next" || info.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err == nil {
				names = append(names, rel)
			}
			if len(names) >= 120 {
				return filepath.SkipAll
			}
			return nil
		})
		if len(names) >= 120 {
			break
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		b.WriteString("\n--- source file map ---\n")
		for _, name := range names {
			b.WriteString(name + "\n")
		}
	}
	return b.String(), nil
}

func redactSensitiveText(s string) string {
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		lower := strings.ToLower(line)
		sensitive := false
		for _, marker := range []string{"api_key", "apikey", "secret", "password", "passwd", "private_key", "access_token", "auth_token", "bearer "} {
			if strings.Contains(lower, marker) {
				sensitive = true
				break
			}
		}
		if sensitive {
			out.WriteString("[REDACTED POSSIBLE SECRET]\n")
		} else {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func generateOllama(ctx context.Context, model, prompt string) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", errors.New("select an Ollama model first")
	}
	payload, _ := json.Marshal(map[string]any{
		"model":      model,
		"prompt":     prompt,
		"stream":     false,
		"keep_alive": "5m",
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:11434/api/generate", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
			return "", fmt.Errorf("Ollama did not return a response within 5 minutes; the model may still be loading or the request may be too large: %w", err)
		}
		return "", fmt.Errorf("Ollama request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("ollama: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Response, nil
}

func generateGemini(ctx context.Context, model, apiKey, prompt string) (string, error) {
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return "", errors.New("Gemini API key is empty")
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}
	payload := map[string]any{
		"contents":         []any{map[string]any{"parts": []any{map[string]any{"text": prompt}}}},
		"generationConfig": map[string]any{"temperature": 0.2, "maxOutputTokens": 500},
	}
	body, _ := json.Marshal(payload)
	endpoint := "https://generativelanguage.googleapis.com/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("gemini: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("Gemini returned no text")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}

func parseSuggestion(text string) Suggestion {
	var s Suggestion
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "SUMMARY:"):
			s.Summary = strings.TrimSpace(line[len("SUMMARY:"):])
		case strings.HasPrefix(strings.ToUpper(line), "DESCRIPTION:"):
			s.Description = strings.TrimSpace(line[len("DESCRIPTION:"):])
		case strings.HasPrefix(strings.ToUpper(line), "COMMIT:"):
			s.CommitMessage = strings.TrimSpace(line[len("COMMIT:"):])
		}
	}
	if s.Summary == "" {
		s.Summary = strings.TrimSpace(text)
	}
	return s
}
