package agent

// GetLLMProcessorCode returns the Go code for the LLM processor
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

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
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
	log.Printf("DEBUG: GEMINI_API_KEY length: %d", len(os.Getenv("GEMINI_API_KEY")))
	log.Printf("DEBUG: ZIZMOR_ISSUES length: %d", len(os.Getenv("ZIZMOR_ISSUES")))
	
	if err := processWorkflows(); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
	log.Println("DEBUG: LLM processor completed successfully")
}

func processWorkflows() error {
	log.Println("DEBUG: Reading prompt file")
	// Read the prompt file
	promptContent, err := ioutil.ReadFile("llm_fix_prompt.md")
	if err != nil {
		return fmt.Errorf("failed to read prompt: %w", err)
	}
	log.Printf("DEBUG: Prompt file size: %d bytes", len(promptContent))

	// Read issues input
	issues := os.Getenv("ZIZMOR_ISSUES")
	if issues == "" {
		issues = "No issues found"
	}
	issuePreview := issues
	if len(issues) > 200 {
		issuePreview = issues[:200] + "..."
	}
	log.Printf("DEBUG: Issues: %s", issuePreview)

	// Initialize Gemini client
	log.Println("DEBUG: Creating Gemini client")
	ctx := context.Background()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return fmt.Errorf("failed to create Gemini client: %w", err)
	}
	defer client.Close()
	log.Println("DEBUG: Gemini client created successfully")

	// Create the model
	model := client.GenerativeModel("gemini-2.0-flash")

	// Find all workflow files
	log.Println("DEBUG: Finding workflow files")
	workflowFiles, err := findWorkflowFiles()
	if err != nil {
		return fmt.Errorf("failed to find workflow files: %w", err)
	}
	log.Printf("DEBUG: Found %d workflow files: %v", len(workflowFiles), workflowFiles)

	// Prepare the enhanced prompt for file fixing
	enhancedPrompt := fmt.Sprintf(` + "`%s\n\nZIZMOR ISSUES TO FIX:\n%s\n\nWORKFLOW FILES FOUND:\n%s\n\nPlease provide your response in the following JSON format:\n{\n  \"explanation\": \"Brief explanation of what fixes were applied\",\n  \"file_changes\": [\n    {\n      \"path\": \"relative/path/to/file.yml\",\n      \"content\": \"complete fixed file content\"\n    }\n  ]\n}\n\nOnly include files that need changes in the file_changes array. Provide the complete corrected content for each file.`" + `,
		string(promptContent), issues, strings.Join(workflowFiles, "\n"))

	// Generate response
	resp, err := model.GenerateContent(ctx, genai.Text(enhancedPrompt))
	if err != nil {
		return fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return fmt.Errorf("no response generated from Gemini")
	}

	// Extract response text
	responseText := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

	// Try to parse JSON response
	var llmResponse LLMResponse
	if err := parseJSONResponse(responseText, &llmResponse); err != nil {
		// If JSON parsing fails, just return the explanation
		fmt.Print(responseText)
		return nil
	}

	// Apply file changes
	for _, change := range llmResponse.FileChanges {
		if err := applyFileChange(change); err != nil {
			log.Printf("Warning: Failed to apply change to %s: %v", change.Path, err)
		}
	}

	// Output explanation
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
	dir := filepath.Dir(change.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write the file
	if err := ioutil.WriteFile(change.Path, []byte(change.Content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", change.Path, err)
	}

	log.Printf("Applied fix to %s", change.Path)
	return nil
}`
}