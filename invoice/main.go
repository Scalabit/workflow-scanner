package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type InvoiceRequest struct {
	Text string `json:"text"`
}

type InvoiceResponse struct {
	ExtractedData string `json:"extracted_data"`
}

func extractTextFromPDF(pdfContent []byte) (string, error) {
	// Write PDF to temporary file
	tmpFile, err := os.CreateTemp("", "invoice-*.pdf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(pdfContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write PDF to temp file: %w", err)
	}
	tmpFile.Close()

	// Use pdftotext to extract text
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdftotext", tmpFile.Name(), "-")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try with python pypdf
		return extractTextWithPythonFallback(pdfContent)
	}

	return string(output), nil
}

func extractTextWithPythonFallback(pdfContent []byte) (string, error) {
	// Fallback to Python PDF extraction if pdftotext unavailable
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pythonScript := `
import sys
try:
    from pypdf import PdfReader
    from io import BytesIO
    import binascii
    
    # Read hex data from stdin and convert to bytes
    hex_data = sys.stdin.read().strip()
    pdf_data = binascii.unhexlify(hex_data)
    
    reader = PdfReader(BytesIO(pdf_data))
    text = ""
    for page in reader.pages:
        text += page.extract_text()
    print(text)
except Exception as e:
    print(f"Error: {e}", file=sys.stderr)
    sys.exit(2)
`

	cmd := exec.CommandContext(ctx, "python3", "-c", pythonScript)
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%x", pdfContent))
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("PDF extraction failed: %w", err)
	}

	return string(output), nil
}

func processWithPhi(text string) (string, error) {
	if len(strings.TrimSpace(text)) == 0 {
		return `{"error":"empty text for processing"}`, nil
	}

	// Set timeout to 5 minutes for model loading and inference
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Use stdin to pass data - avoids escaping issues entirely
	pythonScript := `
import sys
import json

try:
    from transformers import AutoTokenizer, AutoModelForCausalLM
    import torch
    
    # Read invoice text from stdin
    invoice_text = sys.stdin.read()
    
    prompt = f"Extract invoice data from this text: {invoice_text}\nReturn as JSON with fields: invoice_number, date, amount, vendor, items"
    
    tokenizer = AutoTokenizer.from_pretrained("microsoft/phi-2")
    model = AutoModelForCausalLM.from_pretrained("microsoft/phi-2", torch_dtype=torch.float32, low_cpu_mem_usage=True, device_map="cpu")
    
    inputs = tokenizer(prompt, return_tensors="pt")
    with torch.no_grad():
        outputs = model.generate(**inputs, max_length=512)
    
    result = tokenizer.decode(outputs[0], skip_special_tokens=True)
    print(result)
except Exception as e:
    print(json.dumps({"error": str(e)}))
    sys.exit(1)
`

	cmd := exec.CommandContext(ctx, "python3", "-c", pythonScript)
	cmd.Stdin = strings.NewReader(text)
	
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Python error: %v, output: %s", err, string(output))
		return "", fmt.Errorf("phi processing failed: %w", err)
	}

	return string(output), nil
}

func processInvoice(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot open file"})
		return
	}
	defer src.Close()

	content, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot read file"})
		return
	}

	text := string(content)
	if strings.HasSuffix(file.Filename, ".pdf") {
		log.Printf("Extracting text from PDF: %s", file.Filename)
		extractedText, err := extractTextFromPDF(content)
		if err != nil {
			log.Printf("PDF extraction error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("PDF extraction failed: %v", err)})
			return
		}
		text = extractedText
	}

	result, err := processWithPhi(text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, InvoiceResponse{ExtractedData: result})
}

func health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func main() {
	// Start email monitoring in background
	go func() {
		log.Println("Starting email monitor...")
		processor := NewEmailProcessor()
		for {
			log.Println("Checking for new emails...")
			if err := processor.ProcessEmails(); err != nil {
				log.Printf("Error processing emails: %v", err)
			}
			time.Sleep(30 * time.Second) // Check every 30 seconds
		}
	}()

	// Start HTTP server
	r := gin.Default()
	r.POST("/process-invoice", processInvoice)
	r.GET("/health", health)

	log.Println("Starting HTTP server on :8000")
	r.Run(":8000")
}