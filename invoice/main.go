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
	tmpFile, err := os.CreateTemp("", "invoice-*.pdf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(pdfContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write PDF to temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to sync temp file: %w", err)
	}
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/pdftotext", "-layout", "-nopgbrk", tmpFile.Name(), "-")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("pdftotext failed: %v, trying Python fallback", err)
		return extractTextWithPythonFallback(pdfContent)
	}

	if len(strings.TrimSpace(string(output))) == 0 {
		log.Printf("pdftotext returned empty text, trying Python fallback")
		return extractTextWithPythonFallback(pdfContent)
	}

	return string(output), nil
}

func extractTextWithPythonFallback(pdfContent []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpFile, err := os.CreateTemp("", "invoice-fallback-*.pdf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(pdfContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write PDF to temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to sync temp file: %w", err)
	}
	tmpFile.Close()

	pythonScript := fmt.Sprintf(`
import sys
from pypdf import PdfReader

try:
    reader = PdfReader("%s")
    text = ""
    for page_num, page in enumerate(reader.pages):
        page_text = page.extract_text()
        if page_text:
            text += f"\n--- Page {page_num + 1} ---\n"
            text += page_text + "\n"
    
    if not text.strip():
        print("No extractable text found in PDF", file=sys.stderr)
        sys.exit(1)
    
    print(text.strip())
except Exception as e:
    print(f"Error: {e}", file=sys.stderr)
    sys.exit(2)
`, tmpFile.Name())

	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-c", pythonScript)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Python PDF extraction failed: %v, trying OCR fallback", err)
		return extractTextWithOCR(pdfContent)
	}

	extractedText := strings.TrimSpace(string(output))
	if len(extractedText) == 0 {
		log.Printf("Python PDF extraction returned empty text, trying OCR fallback")
		return extractTextWithOCR(pdfContent)
	}

	return extractedText, nil
}

func extractTextWithOCR(pdfContent []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Convert PDF to PNG using pdftoppm, then OCR with tesseract
	tmpFile, err := os.CreateTemp("", "invoice-ocr-*.pdf")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(pdfContent); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to write PDF to temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("failed to sync temp file: %w", err)
	}
	tmpFile.Close()

	// Convert PDF to image
	pngFile := strings.TrimSuffix(tmpFile.Name(), ".pdf") + ".png"
	defer os.Remove(pngFile)
	
	cmd := exec.CommandContext(ctx, "/usr/bin/pdftoppm", "-png", "-singlefile", tmpFile.Name(), strings.TrimSuffix(pngFile, ".png"))
	if err := cmd.Run(); err != nil {
		log.Printf("pdftoppm conversion failed: %v", err)
		return "Unable to extract text from PDF. This appears to be a corrupted, password-protected, or image-only PDF file.", nil
	}

	// OCR with tesseract
	cmd = exec.CommandContext(ctx, "/usr/bin/tesseract", pngFile, "stdout", "-l", "eng")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("OCR extraction failed: %v", err)
		return "Unable to extract text from PDF using OCR. This may be a low-quality image or corrupted file.", nil
	}

	extractedText := strings.TrimSpace(string(output))
	if len(extractedText) == 0 {
		log.Printf("OCR returned empty text")
		return "PDF appears to contain no readable text. This may be a blank document or very low quality scan.", nil
	}

	log.Printf("Successfully extracted text using OCR: %d characters", len(extractedText))
	return extractedText, nil
}

func processWithPhi(text string) (string, error) {
	if len(strings.TrimSpace(text)) == 0 {
		return `{"error":"empty text for processing"}`, nil
	}

	if strings.Contains(text, "Unable to extract text from PDF") {
		return `{"error":"corrupted_pdf","message":"Unable to extract invoice data from corrupted, password-protected, or image-only PDF file"}`, nil
	}
	
	if strings.Contains(text, "PDF appears to contain no extractable text") {
		return `{"error":"image_only_pdf","message":"PDF appears to contain no extractable text - likely an image-only document"}`, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pythonScript := `
import sys
import json

try:
    from transformers import AutoTokenizer, AutoModelForCausalLM
    import torch
    
    # Read invoice text from stdin
    invoice_text = sys.stdin.read()
    
    tokenizer = AutoTokenizer.from_pretrained("microsoft/Phi-3.5-mini-instruct")
    
    messages = [
        {"role": "user", "content": f"Read this invoice and extract ALL data as JSON. Find the invoice number, date, due date, company name, total amount, and extract EVERY item from any product tables or lists - there may be multiple items.\n\nInvoice text:\n{invoice_text}\n\nReturn only valid JSON with ALL the extracted data including every single item. Make sure to include 'due_date' field if present:"}
    ]
    prompt = tokenizer.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
    model = AutoModelForCausalLM.from_pretrained(
        "microsoft/Phi-3.5-mini-instruct", 
        torch_dtype=torch.float32,
        device_map="cpu"
    )
    
    inputs = tokenizer(prompt, return_tensors="pt")
    with torch.no_grad():
        outputs = model.generate(**inputs, max_new_tokens=2048, do_sample=False, pad_token_id=tokenizer.eos_token_id)
    
    result = tokenizer.decode(outputs[0], skip_special_tokens=True)
    
    # Clean up to free memory
    del model
    del tokenizer
    del inputs
    del outputs
    import gc
    gc.collect()
    
    import re
    json_output = None
    markdown_match = re.search(r'` + "`" + `json\s*(.*?)\s*` + "`" + `', result, re.DOTALL)
    if markdown_match:
        json_output = markdown_match.group(1).strip()
    else:
        patterns = [
            r'JSON:\s*(\{.*?\})',           # JSON: {...}
            r'\{[^{}]*"invoice_number"[^{}]*\}',  # Look for invoice_number field
            r'\{.*?\}',                     # Any JSON object
        ]
        
        for pattern in patterns:
            json_match = re.search(pattern, result, re.DOTALL)
            if json_match:
                json_output = json_match.group(1) if json_match.lastindex else json_match.group(0)
                break
    
    if json_output:
        print(json_output)
    else:
        print(json.dumps({"error": "No valid JSON found in model output", "raw_output": result[:1000]}))
except Exception as e:
    print(json.dumps({"error": str(e)}))
    sys.exit(1)
`

	cmd := exec.CommandContext(ctx, "/usr/bin/python3", "-c", pythonScript)
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
	go func() {
		log.Println("Starting email monitor...")
		processor := NewEmailProcessor()
		for {
			log.Println("Checking for new emails...")
			if err := processor.ProcessEmails(); err != nil {
				log.Printf("Error processing emails: %v", err)
			}
			time.Sleep(30 * time.Second)
		}
	}()

	r := gin.Default()
	r.POST("/process-invoice", processInvoice)
	r.GET("/health", health)

	log.Println("Starting HTTP server on :8000")
	r.Run(":8000")
}