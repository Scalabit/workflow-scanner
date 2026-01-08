package agent

// GetLLMProcessorCode returns the Go code for the LLM processor.
func GetLLMProcessorCode() string {
	return `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

type FileChange struct {
	Path    string ` + "`json:\"path\"`" + `
	Content string ` + "`json:\"content\"`" + `
}

type LLMResponse struct {
	Explanation string       ` + "`json:\"explanation\"`" + `
	FileChanges []FileChange ` + "`json:\"file_changes\"`" + `
}

func main() {
	log.Println("DEBUG: Starting LLM processor")
	log.Printf("DEBUG: OPENAI_API_KEY length: %d", len(os.Getenv("OPENAI_API_KEY")))
	log.Printf("DEBUG: ANTHROPIC_API_KEY length: %d", len(os.Getenv("ANTHROPIC_API_KEY")))
	log.Printf("DEBUG: GEMINI_API_KEY length: %d", len(os.Getenv("GEMINI_API_KEY")))
	log.Printf("DEBUG: MODEL: %s", os.Getenv("MODEL"))
	log.Printf("DEBUG: ZIZMOR_ISSUES length: %d", len(os.Getenv("ZIZMOR_ISSUES")))
	
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

	client, ctx, cancel, err := createOpenAIClient()
	if err != nil {
		return err
	}
	defer cancel()

	workflowFiles, err := findWorkflowFiles()
	if err != nil {
		return fmt.Errorf("failed to find workflow files: %w", err)
	}
	log.Printf("DEBUG: Found %d workflow files: %v", len(workflowFiles), workflowFiles)

	enhancedPrompt := buildEnhancedPrompt(promptContent, issues, workflowFiles)

	resp, err := callOpenAI(ctx, client, enhancedPrompt)
	if err != nil {
		return err
	}

	return processOpenAIResponse(resp)
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

func buildEnhancedPrompt(promptContent []byte, issues string, workflowFiles []string) string {
	return fmt.Sprintf(` + "`%s\n\nZIZMOR ISSUES TO FIX:\n%s\n\nWORKFLOW FILES FOUND:\n%s\n\nPlease provide your response in the following JSON format:\n{\n  \"explanation\": \"Brief explanation of what fixes were applied\",\n  \"file_changes\": [\n    {\n      \"path\": \"relative/path/to/file.yml\",\n      \"content\": \"complete fixed file content\"\n    }\n  ]\n}\n\nOnly include files that need changes in the file_changes array. Provide the complete corrected content for each file.`" + `,
		string(promptContent), issues, strings.Join(workflowFiles, "\n"))
}

func callOpenAI(ctx context.Context, client *openai.Client, enhancedPrompt string) (*openai.ChatCompletionResponse, error) {
	log.Println("DEBUG: Sending request to OpenAI API")
	const (
		maxTokens      = 4000
		lowTemperature = 0.1
	)
	
	// Get model from environment or default to gpt-4o
	model := os.Getenv("MODEL")
	if model == "" {
		model = "gpt-4o"
	}
	
	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: enhancedPrompt,
			},
		},
		MaxTokens:   maxTokens,
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
	// Ensure the directory exists
	const (
		dirPermissions  = 0755
		filePermissions = 0644
	)
	dir := filepath.Dir(change.Path)
	if err := os.MkdirAll(dir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write the file
	if err := ioutil.WriteFile(change.Path, []byte(change.Content), filePermissions); err != nil {
		return fmt.Errorf("failed to write file %s: %w", change.Path, err)
	}

	log.Printf("Applied fix to %s", change.Path)

	return nil
}`
}
