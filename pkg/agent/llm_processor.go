package agent

// GetLLMProcessorCode returns the Go code for the LLM processor.
//nolint:maintidx // This function intentionally contains large embedded code string
func GetLLMProcessorCode() string {
	return `package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

type LineChange struct {
	LineNumber int    ` + "`json:\"line_number\"`" + `
	OldLine    string ` + "`json:\"old_line\"`" + `
	NewLine    string ` + "`json:\"new_line\"`" + `
}

type FileChange struct {
	Path    string       ` + "`json:\"path\"`" + `
	Changes []LineChange ` + "`json:\"changes\"`" + `
}

type LLMResponse struct {
	Explanation string       ` + "`json:\"explanation\"`" + `
	FileChanges []FileChange ` + "`json:\"file_changes\"`" + `
}

type ZizmorFinding struct {
	Ident     string ` + "`json:\"ident\"`" + `
	Desc      string ` + "`json:\"desc\"`" + `
	Locations []struct {
		Symbolic struct {
			Key struct {
				Local struct {
					GivenPath string ` + "`json:\"given_path\"`" + `
				} ` + "`json:\"Local\"`" + `
			} ` + "`json:\"key\"`" + `
			Annotation string ` + "`json:\"annotation\"`" + `
		} ` + "`json:\"symbolic\"`" + `
		Concrete struct {
			Location struct {
				StartPoint struct {
					Row int ` + "`json:\"row\"`" + `
				} ` + "`json:\"start_point\"`" + `
			} ` + "`json:\"location\"`" + `
		} ` + "`json:\"concrete\"`" + `
	} ` + "`json:\"locations\"`" + `
}

type FileFinding struct {
	Path     string
	Findings []ZizmorFinding
}

func main() {
	log.Println("DEBUG: Starting LLM processor")
	log.Printf("DEBUG: OPENAI_API_KEY length: %d", len(os.Getenv("OPENAI_API_KEY")))
	log.Printf("DEBUG: ANTHROPIC_API_KEY length: %d", len(os.Getenv("ANTHROPIC_API_KEY")))
	log.Printf("DEBUG: GEMINI_API_KEY length: %d", len(os.Getenv("GEMINI_API_KEY")))
	log.Printf("DEBUG: MODEL: %s", os.Getenv("MODEL"))
	
	if err := processWorkflows(); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
	log.Println("DEBUG: LLM processor completed successfully")
}

func processWorkflows() error {
	promptContent, issues, err := loadInputData()
	if err != nil {
		return err
	}

	// Parse ZIZMOR findings and group by file
	fileFindings, err := groupFindingsByFile(issues)
	if err != nil {
		return fmt.Errorf("failed to parse ZIZMOR findings: %w", err)
	}
	log.Printf("DEBUG: Grouped findings into %d files", len(fileFindings))

	// Process each file separately
	allExplanations := []string{}
	for _, fileFinding := range fileFindings {
		log.Printf("DEBUG: Processing file: %s with %d findings", fileFinding.Path, len(fileFinding.Findings))
		
		explanation, err := processFile(promptContent, fileFinding)
		if err != nil {
			log.Printf("Warning: Failed to process %s: %v", fileFinding.Path, err)
			continue
		}
		
		if explanation != "" {
			allExplanations = append(allExplanations, fmt.Sprintf("[%s] %s", fileFinding.Path, explanation))
		}
	}

	// Print combined explanations
	if len(allExplanations) > 0 {
		fmt.Print(strings.Join(allExplanations, "\n"))
	} else {
		fmt.Print("No fixes applied")
	}

	return nil
}

func groupFindingsByFile(issues string) ([]FileFinding, error) {
	var findings []ZizmorFinding
	if err := json.Unmarshal([]byte(issues), &findings); err != nil {
		return nil, err
	}

	fileMap := make(map[string][]ZizmorFinding)
	for _, finding := range findings {
		for _, loc := range finding.Locations {
			path := loc.Symbolic.Key.Local.GivenPath
			if path != "" {
				fileMap[path] = append(fileMap[path], finding)
			}
		}
	}

	result := []FileFinding{}
	for path, finds := range fileMap {
		result = append(result, FileFinding{
			Path:     path,
			Findings: finds,
		})
	}

	return result, nil
}

func processFile(promptContent []byte, fileFinding FileFinding) (string, error) {
	// Read the specific file
	content, err := os.ReadFile(fileFinding.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Build focused prompt for this file only
	prompt := buildFilePrompt(promptContent, fileFinding, string(content))

	// Call LLM
	if os.Getenv("OPENAI_API_KEY") != "" {
		return callOpenAIForFile(prompt, fileFinding.Path)
	} else if os.Getenv("GEMINI_API_KEY") != "" {
		return callGeminiForFile(prompt, fileFinding.Path)
	} else if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return callAnthropicForFile(prompt, fileFinding.Path)
	}

	return "", fmt.Errorf("no API key found")
}

func buildFilePrompt(promptContent []byte, fileFinding FileFinding, fileContent string) string {
	// Add line numbers to file
	lines := strings.Split(fileContent, "\n")
	var numberedContent strings.Builder
	for i, line := range lines {
		numberedContent.WriteString(fmt.Sprintf("%3d | %s\n", i+1, line))
	}

	// Format findings for this file
	var findingsText strings.Builder
	for _, finding := range fileFinding.Findings {
		for _, loc := range finding.Locations {
			row := loc.Concrete.Location.StartPoint.Row
			annotation := loc.Symbolic.Annotation
			findingsText.WriteString(fmt.Sprintf("- Line %d: %s (%s)\n", row, finding.Desc, annotation))
		}
	}

	return fmt.Sprintf("%s\n\nFILE: %s\n%s\n\nISSUES TO FIX:\n%s\n\nReturn ONLY line changes for this file.",
		string(promptContent), fileFinding.Path, numberedContent.String(), findingsText.String())
}

func callOpenAIForFile(prompt, filePath string) (string, error) {
	client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	model := os.Getenv("MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		MaxCompletionTokens: 4000,
		Temperature:         0.1,
	})
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	responseText := resp.Choices[0].Message.Content
	
	var llmResponse LLMResponse
	if err := parseJSONResponse(responseText, &llmResponse); err != nil {
		log.Printf("Warning: Failed to parse JSON for %s, skipping", filePath)
		return "", nil
	}

	// Apply changes to this file
	for _, fileChange := range llmResponse.FileChanges {
		if err := applyFileChange(fileChange); err != nil {
			log.Printf("Warning: Failed to apply changes to %s: %v", fileChange.Path, err)
		}
	}

	return llmResponse.Explanation, nil
}

func callGeminiForFile(prompt, filePath string) (string, error) {
	// Simplified - return empty for now
	return "", fmt.Errorf("Gemini not implemented for file-by-file processing yet")
}

func callAnthropicForFile(prompt, filePath string) (string, error) {
	// Simplified - return empty for now
	return "", fmt.Errorf("Anthropic not implemented for file-by-file processing yet")
}

func loadInputData() ([]byte, string, error) {
	log.Println("DEBUG: Reading prompt file")
	promptContent, err := ioutil.ReadFile("llm_fix_prompt.md")
	if err != nil {
		return nil, "", fmt.Errorf("failed to read prompt: %w", err)
	}
	log.Printf("DEBUG: Prompt file size: %d bytes", len(promptContent))

	issues := os.Getenv("ZIZMOR_ISSUES")
	if issues == "" {
		issues = "No issues found"
	}
	const maxIssuePreviewLength = 200
	issuePreview := issues
	if len(issues) > maxIssuePreviewLength {
		issuePreview = issues[:maxIssuePreviewLength] + "..."
	}
	log.Printf("DEBUG: Issues: %s", issuePreview)

	return promptContent, issues, nil
}

func createOpenAIClient() (*openai.Client, context.Context, context.CancelFunc, error) {
	log.Println("DEBUG: Creating OpenAI client")
	ctx := context.Background()
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, ctx, func() {}, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}
	
	const apiTimeoutMinutes = 5
	ctx, cancel := context.WithTimeout(ctx, time.Minute*apiTimeoutMinutes)
	
	client := openai.NewClient(apiKey)
	log.Println("DEBUG: OpenAI client created successfully")

	return client, ctx, cancel, nil
}

func buildEnhancedPrompt(promptContent []byte, issues string, workflowContents map[string]string) string {
	// Build workflow files section with line numbers
	var workflowSection strings.Builder
	for path, content := range workflowContents {
		lines := strings.Split(content, "\n")
		workflowSection.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n", path))
		for i, line := range lines {
			workflowSection.WriteString(fmt.Sprintf("%3d | %s\n", i+1, line))
		}
	}

	return fmt.Sprintf("%s\n\nZIZMOR ISSUES:\n%s\n\nWORKFLOW FILES:\n%s",
		string(promptContent), issues, workflowSection.String())
}

func callOpenAI(ctx context.Context, client *openai.Client, enhancedPrompt string) (*openai.ChatCompletionResponse, error) {
	log.Println("DEBUG: Sending request to OpenAI API")
	const (
		maxTokens      = 4000
		lowTemperature = 0.1
	)
	
	// Get model from environment or default to gpt-4.1
	model := os.Getenv("MODEL")
	if model == "" {
		model = "gpt-4.1"
	}
	
	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: enhancedPrompt,
			},
		},
		MaxCompletionTokens: maxTokens,
		Temperature: lowTemperature,
	}
	
	resp, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}
	log.Println("DEBUG: Received response from OpenAI API")

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response generated from OpenAI")
	}
	log.Printf("DEBUG: Response has %d choices", len(resp.Choices))

	return &resp, nil
}

func processOpenAIResponse(resp *openai.ChatCompletionResponse) error {
	responseText := resp.Choices[0].Message.Content

	log.Println("DEBUG: Parsing response as JSON")
	var llmResponse LLMResponse
	if err := parseJSONResponse(responseText, &llmResponse); err != nil {
		log.Printf("DEBUG: JSON parsing failed: %v", err)
		log.Println("DEBUG: Returning raw response text")
		fmt.Print(responseText)
		return nil
	}

	log.Printf("DEBUG: Applying %d file changes", len(llmResponse.FileChanges))
	for i, change := range llmResponse.FileChanges {
		log.Printf("DEBUG: Applying change %d/%d to %s", i+1, len(llmResponse.FileChanges), change.Path)
		if err := applyFileChange(change); err != nil {
			log.Printf("Warning: Failed to apply change to %s: %v", change.Path, err)
		}
	}

	log.Printf("DEBUG: Returning explanation: %d chars", len(llmResponse.Explanation))
	fmt.Print(llmResponse.Explanation)

	return nil
}

func findWorkflowFiles() ([]string, error) {
	var files []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

func readWorkflowContents(files []string) (map[string]string, error) {
	contents := make(map[string]string)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		contents[file] = string(content)
	}
	return contents, nil
}

func parseJSONResponse(responseText string, llmResponse *LLMResponse) error {
	// Find JSON content between ` + "```json and ```" + ` markers
	start := strings.Index(responseText, "` + "```json" + `")
	if start == -1 {
		start = strings.Index(responseText, "{")
	} else {
		start += 7 // skip ` + "```json" + `
	}

	end := strings.LastIndex(responseText, "}")
	if start == -1 || end == -1 || start >= end {

		return fmt.Errorf("no valid JSON found in response")
	}

	jsonStr := strings.TrimSpace(responseText[start : end+1])

	return json.Unmarshal([]byte(jsonStr), llmResponse)
}

func applyFileChange(change FileChange) error {
	// Read the current file
	content, err := os.ReadFile(change.Path)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", change.Path, err)
	}

	lines := strings.Split(string(content), "\n")
	
	// Apply each line change
	for _, lineChange := range change.Changes {
		if lineChange.LineNumber < 1 || lineChange.LineNumber > len(lines) {
			log.Printf("Warning: Line number %d out of range for %s (file has %d lines)", 
				lineChange.LineNumber, change.Path, len(lines))
			continue
		}
		
		// Verify the old line matches (for safety)
		actualLine := lines[lineChange.LineNumber-1]
		expectedLine := strings.TrimSpace(lineChange.OldLine)
		if strings.TrimSpace(actualLine) != expectedLine {
			log.Printf("Warning: Line %d doesn't match expected content in %s", lineChange.LineNumber, change.Path)
			log.Printf("  Expected: %q", expectedLine)
			log.Printf("  Actual:   %q", strings.TrimSpace(actualLine))
			log.Printf("  Applying change anyway...")
		}
		
		// Apply the change - handle multi-line replacements
		newLines := strings.Split(lineChange.NewLine, "\\n")
		if len(newLines) == 1 {
			// Simple single-line replacement
			lines[lineChange.LineNumber-1] = lineChange.NewLine
		} else {
			// Multi-line replacement: remove old line, insert new lines
			before := lines[:lineChange.LineNumber-1]
			after := lines[lineChange.LineNumber:]
			lines = append(before, append(newLines, after...)...)
		}
		
		log.Printf("Applied change to %s at line %d", change.Path, lineChange.LineNumber)
	}

	// Write the modified file back
	const filePermissions = 0644
	modifiedContent := strings.Join(lines, "\n")
	if err := os.WriteFile(change.Path, []byte(modifiedContent), filePermissions); err != nil {
		return fmt.Errorf("failed to write file %s: %w", change.Path, err)
	}

	log.Printf("Successfully applied %d changes to %s", len(change.Changes), change.Path)
	return nil
}

func callGemini(enhancedPrompt string) error {
	apiKey := os.Getenv("GEMINI_API_KEY")
	model := os.Getenv("MODEL")
	if model == "" {
		model = "gemini-2.5-pro"
	}
	
	log.Printf("DEBUG: Calling Gemini API with model: %s", model)
	
	// Gemini API request structure
	requestBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{
						"text": enhancedPrompt,
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":   0.1,
			"maxOutputTokens": 32000,
		},
	}
	
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to call Gemini API: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("DEBUG: Gemini API error response: %s", string(body))
		return fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(body))
	}
	
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode Gemini response: %w", err)
	}
	
	// Debug: Log the full response structure
	responseJSON, _ := json.MarshalIndent(response, "", "  ")
	log.Printf("DEBUG: Full Gemini response: %s", string(responseJSON))
	
	// Extract text from Gemini response
	candidates, ok := response["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return fmt.Errorf("no candidates in Gemini response")
	}
	
	candidate := candidates[0].(map[string]interface{})
	content := candidate["content"].(map[string]interface{})
	parts := content["parts"].([]interface{})
	if len(parts) == 0 {
		return fmt.Errorf("no parts in Gemini response")
	}
	
	part := parts[0].(map[string]interface{})
	text := part["text"].(string)
	
	log.Printf("DEBUG: Gemini response received, length: %d", len(text))
	
	// Parse and process the response using the same logic as OpenAI
	var llmResponse LLMResponse
	if err := parseJSONResponse(text, &llmResponse); err != nil {
		log.Printf("DEBUG: Gemini JSON parsing failed: %v", err)
		log.Printf("DEBUG: Raw Gemini response: %s", text)
		return fmt.Errorf("failed to parse JSON from Gemini response: %w", err)
	}
	
	return processGenericResponse(&llmResponse)
}

func callAnthropic(enhancedPrompt string) error {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	model := os.Getenv("MODEL")
	if model == "" {
		model = "claude-sonnet-4-5"
	}
	
	log.Printf("DEBUG: Calling Anthropic API with model: %s", model)
	
	// Anthropic API request structure
	requestBody := map[string]interface{}{
		"model": model,
		"max_tokens": 4000,
		"temperature": 0.1,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": enhancedPrompt,
			},
		},
	}
	
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}
	
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Anthropic API: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("Anthropic API returned status %d", resp.StatusCode)
	}
	
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode Anthropic response: %w", err)
	}
	
	// Extract text from Anthropic response
	content, ok := response["content"].([]interface{})
	if !ok || len(content) == 0 {
		return fmt.Errorf("no content in Anthropic response")
	}
	
	contentItem := content[0].(map[string]interface{})
	text := contentItem["text"].(string)
	
	log.Printf("DEBUG: Anthropic response received, length: %d", len(text))
	
	// Parse and process the response using the same logic as OpenAI
	var llmResponse LLMResponse
	if err := parseJSONResponse(text, &llmResponse); err != nil {
		return fmt.Errorf("failed to parse JSON from Anthropic response: %w", err)
	}
	
	return processGenericResponse(&llmResponse)
}

func processGenericResponse(llmResponse *LLMResponse) error {
	log.Printf("DEBUG: Applying %d file changes", len(llmResponse.FileChanges))
	for i, change := range llmResponse.FileChanges {
		log.Printf("DEBUG: Applying change %d/%d to %s", i+1, len(llmResponse.FileChanges), change.Path)
		if err := applyFileChange(change); err != nil {
			log.Printf("Warning: Failed to apply change to %s: %v", change.Path, err)
		}
	}

	log.Printf("DEBUG: Returning explanation: %d chars", len(llmResponse.Explanation))
	fmt.Print(llmResponse.Explanation)

	return nil
}
`
}
